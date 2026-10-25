"""Plan, then execute: a loop of our own (docs/plan-and-execute.md, step 1).

For each task, a fresh model with no tools gets the task, the tools' input
schemas and the call-tree view (as probe.py -schemas gives them), and
answers with the whole change as one gorefactor batch; or first asks to
read the declarations it must write code for, once. The batch runs on a
fresh copy of the module, then the module's own tests. A refusal or a
failing test goes back in the same conversation (claude -p --resume, so
the prefix stays cached) with the source of each declaration it names,
for the batch again, at most -repairs times. The task's hidden tests
judge the last batch that ran.

Run from the repository root:

    python3 eval-tasks/learning/loop.py -tasks eval-tasks/derivations-freight.json \
        -only freight-dispatch,freight-routes,freight-tail,freight-run \
        -models haiku -runs 4 -repairs 2 -out eval-runs/loop-freight

-models ref answers with the reference batch (eval-tasks/learning/refs),
costs nothing, and checks the loop itself.
"""
import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import probe  # noqa: E402

ROOT = os.getcwd()

PLAN = """You are making a change to a Go module through gorefactor, a tool
that runs refactorings and edits. You have no tools in this conversation:
you answer with JSON, and a program runs it and tells you what happened.

The tools (each step of a batch is {{"tool": NAME, "args": {{...}}}}, with
the arguments in that tool's input schema):

{tools}

The module, as a call tree (names, signatures and doc comments; no bodies):

{view}

The change:
{prompt}

Answer with one JSON object and nothing else, either
{{"batch": {{"steps": [the whole change as gorefactor steps, in order]}}}}
or, only if you must write code whose current form you cannot see here
(a body you rewrite, a format you copy), first
{{"read": ["pkg.Name" or "pkg.Type.Method", ...]}}
to be sent their source, once. Prefer refactorings that derive the change
(fold, inline_function, rename, extract_struct, ...) to edits that rewrite
code. Address declarations by name where a tool allows it; a batch with
check "end" may pass through code that does not compile yet. After it
runs, the module's own tests run; you get one message back with what
failed, if anything."""

READ = """The source you asked for:

{source}

Answer with the batch: {{"batch": {{"steps": [...]}}}}."""

UNPARSED = """Your answer was not one JSON object with a "batch" key that parses.
Answer with that JSON alone, nothing before or after it:
{"batch": {"steps": [...]}}."""

REPAIR = """The batch did not succeed:

{what}

{source}Nothing of it was kept: the next batch runs on the module as it was
at the start. Answer with the whole batch again, corrected:
{{"batch": {{"steps": [...]}}}}."""


def declarations(module):
    """Maps "pkg.Name" and "file" to (file, start, end) of each top-level
    function and method of module."""
    out = {}
    for f in probe.funcs(module):
        pkg = os.path.basename(os.path.dirname(f["file"]))
        out[pkg + "." + f["name"]] = (f["file"], f["start"], f["end"])
    return out


def source(module, decls, names):
    """The source of the named declarations, each headed by its place."""
    parts = []
    for n in names:
        d = decls.get(n) or decls.get(n.split(".", 1)[-1])
        if d is None:
            parts.append(f"== {n}: no such function or method")
            continue
        file, start, end = d
        lines = open(os.path.join(module, file)).read().splitlines()
        parts.append(f"== {n} ({file}:{start})\n" + "\n".join(lines[start - 1:end]))
    return "\n\n".join(parts)


PLACE = re.compile(r"([\w./-]+\.go):(\d+)")


def named_places(module, text, limit=8):
    """The source around each file:line that text names, as the enclosing
    top-level declaration for code and five lines around it otherwise
    (a test's composite literal at package level), at most limit."""
    parts, seen = [], set()
    for file, line in PLACE.findall(text):
        line = int(line)
        path = os.path.join(module, file)
        if not os.path.isfile(path) or (file, line) in seen:
            continue
        seen.add((file, line))
        lines = open(path).read().splitlines()
        start = line
        while start > 1 and not re.match(r"(func|type|var|const) ", lines[start - 1]):
            start -= 1
        end = line
        while end < len(lines) and lines[end - 1] != "}" and not (end == start and lines[end - 1].endswith("}")):
            end += 1
        if end - start > 60:
            start, end = max(1, line - 5), min(len(lines), line + 5)
        parts.append(f"== {file}:{start}-{end}\n" + "\n".join(lines[start - 1:end]))
        if len(parts) == limit:
            break
    return "".join(p + "\n\n" for p in parts)


def run_batch(binary, module, batch):
    """Runs batch on a fresh copy of module, then go vet and the module's
    own tests. Returns (copy, ok, what failed); after a refusal, the copy
    is as it was just before the refused step."""
    d = probe.copy_module(module)
    path = os.path.join(d, ".batch.json")
    json.dump(batch, open(path, "w"))
    r = probe.sh([binary, "-C", d, "batch", "@" + path])
    os.remove(path)
    out = (r.stdout + r.stderr).strip()
    if not out.startswith("APPLIED"):
        shutil.rmtree(d)
        return before_step(binary, module, batch, out), False, out[:3000] or "no output"
    t = probe.sh(["sh", "-c", "go vet ./... && go test ./..."], cwd=d)
    if t.returncode != 0:
        return d, False, "The batch applied, but the module's tests failed:\n" + (t.stdout + t.stderr)[:3000]
    return d, True, ""


def before_step(binary, module, batch, refusal):
    """A copy of module with the steps before the one refused applied, so
    that the places the refusal names are where it found them."""
    m = re.match(r"REFUSED batch at step (\d+) of", refusal)
    d = probe.copy_module(module)
    if m and int(m.group(1)) > 1:
        path = os.path.join(d, ".batch.json")
        json.dump(dict(batch, steps=batch["steps"][:int(m.group(1)) - 1]), open(path, "w"))
        probe.sh([binary, "-C", d, "batch", "@" + path])
        os.remove(path)
    return d


def hidden(d, tests):
    shutil.copytree(os.path.join(ROOT, "eval-tasks", tests), d, dirs_exist_ok=True)
    return probe.sh(["sh", "-c", "go vet ./... && go test ./..."], cwd=d).returncode == 0


class Model:
    """One conversation with claude -p, continued with --resume."""

    def __init__(self, name, cwd):
        self.name, self.cwd, self.session, self.cost, self.requests = name, cwd, None, 0.0, 0

    def ask(self, text):
        # No tools and no MCP servers: the model answers with JSON, and
        # Claude Code's tools would only add to every request's context.
        cmd = ["claude", "-p", "--model", self.name, "--output-format", "json", "--tools", "", "--strict-mcp-config"]
        if self.session:
            cmd += ["--resume", self.session]
        r = probe.sh(cmd, cwd=self.cwd, stdin=text)
        self.requests += 1
        try:
            res = json.loads(r.stdout)
        except json.JSONDecodeError:
            return None, (r.stdout + r.stderr)[:300]
        self.session = res.get("session_id", self.session)
        self.cost += res.get("total_cost_usd", 0.0)
        return res.get("result", ""), ""


class Ref:
    """Answers with the reference batch: checks the loop for free."""

    def __init__(self, ref):
        self.ref, self.cost, self.requests = ref, 0.0, 0

    def ask(self, text):
        self.requests += 1
        return json.dumps({"batch": self.ref}), ""


def attempt(task, model, tools, binary, repairs, log):
    module = os.path.join(ROOT, "eval-tasks", task["dir"])
    decls = declarations(module)
    text = PLAN.format(tools=tools, view=probe.view("calltree", module), prompt=task["prompt"])
    row = {"reads": 0, "repairs": 0, "refusals": [], "error": ""}
    answer, err = model.ask(text)
    ans = probe.parse(answer)
    log.append(answer or err)
    if ans and ans.get("read") and not ans.get("batch"):
        row["reads"] = len(ans["read"])
        answer, err = model.ask(READ.format(source=source(module, decls, ans["read"])))
        ans = probe.parse(answer)
        log.append(answer or err)
    batch, d, ok = {}, None, False
    for n in range(repairs + 1):
        if not ans or not isinstance(ans.get("batch"), dict):
            row["refusals"].append(err or "no batch in the answer")
            if n == repairs:
                row["error"] = err or "no batch in the answer"
                break
            row["repairs"] += 1
            answer, err = model.ask(UNPARSED)
            ans = probe.parse(answer)
            log.append(answer or err)
            continue
        batch = ans["batch"]
        if d:
            shutil.rmtree(d)
        d, ok, what = run_batch(binary, module, batch)
        if ok or n == repairs:
            if not ok:
                row["refusals"].append(what.splitlines()[0][:200])
            break
        row["repairs"] += 1
        row["refusals"].append(what.splitlines()[0][:200])
        answer, err = model.ask(REPAIR.format(what=what, source=named_places(d, what)))
        ans = probe.parse(answer)
        log.append(answer or err)
    row["applied"] = ok
    row["passed"] = bool(ok and d and hidden(d, task["tests"]))
    if d:
        shutil.rmtree(d)
    row.update(steps=len(batch.get("steps", [])), written=probe.written(batch), tools=probe.tools_of(batch))
    return row


def main():
    p = argparse.ArgumentParser()
    p.add_argument("-tasks", required=True)
    p.add_argument("-only", default="")
    p.add_argument("-models", default="haiku")
    p.add_argument("-runs", type=int, default=4)
    p.add_argument("-repairs", type=int, default=2)
    p.add_argument("-out", required=True)
    a = p.parse_args()
    os.makedirs(a.out, exist_ok=True)
    tmp = tempfile.mkdtemp(prefix="loop-")
    binary = os.path.join(tmp, "refactor")
    probe.sh(["go", "build", "-o", binary, "./cmd/refactor"])
    tools = probe.schemas()
    tasks = [t for t in json.load(open(a.tasks)) if not a.only or t["name"] in a.only.split(",")]
    results = open(os.path.join(a.out, "results.jsonl"), "a")
    for t in tasks:
        ref = json.load(open(os.path.join(ROOT, "eval-tasks/learning/refs", t["name"] + ".json")))
        for m in a.models.split(","):
            for n in range(1, a.runs + 1):
                empty = tempfile.mkdtemp(prefix="loop-empty-")
                model = Ref(ref) if m == "ref" else Model(m, empty)
                log = []
                row = {"task": t["name"], "model": m, "run": n}
                row.update(attempt(t, model, tools, binary, a.repairs, log))
                row.update(cost=model.cost, requests=model.requests)
                with open(os.path.join(a.out, f"{t['name']}-{m}-{n}.txt"), "w") as f:
                    f.write("\n\n==== next answer ====\n\n".join(log))
                results.write(json.dumps(row) + "\n")
                results.flush()
                shutil.rmtree(empty)
                print(json.dumps({k: row[k] for k in ("task", "model", "run", "passed", "requests", "repairs", "reads", "cost")}), flush=True)
    shutil.rmtree(tmp)


if __name__ == "__main__":
    main()
