"""Plan, then execute: a loop of our own (docs/plan-and-execute.md, step 1).

For each task, a fresh model with no tools gets the task, the tools' input
schemas and the call-tree view (as probe.py -schemas gives them), and
answers with the whole change as one gorefactor batch; or first asks to
read declarations, which it may also do in place of any later batch, at
most three times; it gets them as inspect shows them, with line numbers,
from the module as the kept batches left it. The batch runs on a
fresh copy of the module, then the module's own tests. A refusal or a
failing test goes back in the same conversation (claude -p --resume, so
the prefix stays cached) with the numbered source of each place it names,
for the batch again, at most -repairs times. When a batch applies and
the tests pass, the model is asked whether the change is done; if not,
its next batch runs on top, at most -stages more. The task's hidden
tests judge what the kept batches left.

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
the arguments the tool takes):

{tools}

The module, as a call tree (names, signatures and doc comments; no bodies):

{view}

The change:
{prompt}

Answer with one JSON object and nothing else:
{{"batch": {{"steps": [the whole change as gorefactor steps, in order]}}}}{read}
Prefer refactorings that derive the change
(fold, inline_function, rename, extract_struct, ...) to edits that rewrite
code. Code you cannot see here need not be written: steps that address
it by name carry it with them (extract_function of a case by function
and case, function_to_method, move_declaration, inline_function), so
move code rather than ask for it or call back into what you move.
Address declarations by name where a tool allows it; a batch with
check "end" may pass through code that does not compile yet. After it
runs, the module's own tests run; you get one message back with what
failed, if anything."""

READ = """The source you asked for, from the module as it is now, with its
line numbers:

{source}

Answer with the batch: {{"batch": {{"steps": [...]}}}}."""

DONE = """The batch applied, and the module's own tests pass. If that
completes the change asked for, answer {"done": true}. If not, answer
with the next batch, which runs on the module as this one left it:
{"batch": {"steps": [...]}}."""

UNPARSED = """Your answer was not one JSON object with a "batch" key that parses.
Answer with that JSON alone, nothing before or after it:
{"batch": {"steps": [...]}}."""

# MAY_READ, in PLAN, lets the first answer ask for source; without it
# (-no-read), source comes only with a repair, for what failed.
MAY_READ = """
or, only if you must write code whose current form you cannot see here
(a body you rewrite or move, a format you copy, lines you address),
{"read": ["Name", "Type.Method" or "pkg.Name", ...]}
for their source with line numbers: functions, methods, types and
variables, tests included. You may answer with a read in place of a
batch, now or after any later message, at most three times in all."""

# READ_HINT ends the later prompts when the model may read.
READ_HINT = """
Or, to see source first: {"read": ["Name", ...]}."""

REPAIR = """The batch did not succeed:

{what}

{source}Nothing of it was kept: the next batch runs on the module as it was
before this one. Answer with that whole batch again, corrected:
{{"batch": {{"steps": [...]}}}}."""


# The sentences most tools' descriptions end with, which the compact
# reference says once.
BY_NAME = "Without line, name alone identifies it as a package-level symbol of file's package (Name, or Type.Method or Type.Field); unlike a line number, a name stays valid after edits."
APPLIES = "Applies the change and returns what changed; undo reverts it."
NOT_STEPS = ("batch", "inspect", "undo")


def compact(tools):
    """The tools as a batch's steps need them: each step's description,
    without the sentences every tool repeats, and its arguments as a
    signature (name: type, a ? for optional), without their descriptions."""
    out = ["Where a tool takes file and name without line, name is a package-level symbol of file's package (Name, Type.Method or Type.Field), and stays valid after earlier steps."]
    for t in tools:
        if t["name"] in NOT_STEPS:
            continue
        s = t["inputSchema"]
        args = []
        for k, v in s.get("properties", {}).items():
            typ = v.get("type", "any")
            if typ == "array":
                typ = (v.get("items") or {}).get("type", "any") + "[]"
            args.append(f"{k}{'' if k in s.get('required', []) else '?'}: {typ}")
        desc = t["description"].replace(BY_NAME, "").replace(APPLIES, "")
        desc = "\n".join(l for l in desc.splitlines() if l.strip())
        out.append(f"{t['name']}({', '.join(args)})\n{desc}")
    return "\n\n".join(out)


def source(binary, module, names):
    """The named declarations of module as inspect shows them, each with
    its file and line numbers."""
    r = subprocess.run([binary, "-C", module, "inspect", json.dumps({"names": [str(n) for n in names]})],
                       capture_output=True, text=True)
    return (r.stdout + r.stderr).strip()


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
        if not 1 <= line <= len(lines):
            continue  # a line of the file as a step left it, past its end now
        start = line
        while start > 1 and not re.match(r"(func|type|var|const) ", lines[start - 1]):
            start -= 1
        end = line
        while end < len(lines) and lines[end - 1] != "}" and not (end == start and lines[end - 1].endswith("}")):
            end += 1
        if end - start > 60:
            start, end = max(1, line - 5), min(len(lines), line + 5)
        numbered = (f"{i:4}  {text}" for i, text in enumerate(lines[start - 1:end], start))
        parts.append(f"== {file}:{start}-{end}\n" + "\n".join(numbered))
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
        self.usage = []  # per request: tokens written to the cache, read from it, and output

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
        # A resumed session reports what the whole session has cost so far.
        self.cost = res.get("total_cost_usd", self.cost)
        u = res.get("usage") or {}
        self.usage.append([u.get("cache_creation_input_tokens", 0), u.get("cache_read_input_tokens", 0), u.get("output_tokens", 0)])
        return res.get("result", ""), ""


class Ref:
    """Answers with the reference batch: checks the loop for free."""

    def __init__(self, ref):
        self.ref, self.cost, self.requests, self.usage = ref, 0.0, 0, []

    def ask(self, text):
        self.requests += 1
        if self.requests > 1:
            return json.dumps({"done": True}), ""
        return json.dumps({"batch": self.ref}), ""


def attempt(task, model, tools, binary, repairs, log, may_read=True, stages=2):
    """One attempt at task. A batch that applies and passes the module's
    tests is kept, and the model is asked whether the change is done; if
    not, its next batch runs on what the kept ones left, at most stages
    more. A refusal or a failing test gets a repair, at most repairs in
    all; the next batch then runs where the failed one began."""
    module = os.path.join(ROOT, "eval-tasks", task["dir"])
    text = PLAN.format(tools=tools, view=probe.view("calltree", module), prompt=task["prompt"],
                       read=MAY_READ if may_read else "")
    row = {"reads": 0, "repairs": 0, "stages": 0, "refusals": [], "error": ""}
    base, batches, left, reads = module, [], repairs, 3  # base: where the next batch runs
    hint = READ_HINT if may_read else ""
    ans = err = None

    def ask(text):
        nonlocal ans, err
        answer, err = model.ask(text)
        ans = probe.parse(answer)
        log.append(answer or err)

    ask(text)
    while True:
        if ans and ans.get("done") is True and batches:
            break
        if may_read and reads and ans and isinstance(ans.get("read"), list) and not ans.get("batch"):
            reads -= 1
            row["reads"] += len(ans["read"])
            ask(READ.format(source=source(binary, base, ans["read"])))
            continue
        if not ans or not isinstance(ans.get("batch"), dict):
            row["refusals"].append(err or "no batch in the answer")
            if left == 0:
                row["error"] = err or "no batch in the answer"
                break
            left -= 1
            row["repairs"] += 1
            ask(UNPARSED)
            continue
        d, ok, what = run_batch(binary, base, ans["batch"])
        if ok:
            batches.append(ans["batch"])
            row["stages"] += 1
            if base != module:
                shutil.rmtree(base)
            base = d
            if row["stages"] > stages:
                break
            ask(DONE + hint)
            continue
        row["refusals"].append(what.splitlines()[0][:200])
        if left == 0:
            shutil.rmtree(d)
            break
        left -= 1
        row["repairs"] += 1
        places = named_places(d, what)
        shutil.rmtree(d)
        ask(REPAIR.format(what=what, source=places) + hint)
    row["applied"] = bool(batches)
    row["passed"] = bool(batches) and hidden(base, task["tests"])
    if base != module:
        shutil.rmtree(base)
    every = {"steps": [s for b in batches for s in b.get("steps", [])]}
    row.update(steps=len(every["steps"]), written=probe.written(every), tools=probe.tools_of(every))
    return row


def main():
    p = argparse.ArgumentParser()
    p.add_argument("-tasks", required=True)
    p.add_argument("-only", default="")
    p.add_argument("-models", default="haiku")
    p.add_argument("-runs", type=int, default=4)
    p.add_argument("-repairs", type=int, default=2)
    p.add_argument("-out", required=True)
    p.add_argument("-no-read", action="store_true", help="the plan cannot ask for source; only repairs send it")
    p.add_argument("-reference", default="compact", choices=["compact", "full"],
                   help="the tools as compact signatures and descriptions, or as the server publishes them")
    p.add_argument("-stages", type=int, default=2, help="batches after the first that the model may send when asked if it is done")
    a = p.parse_args()
    os.makedirs(a.out, exist_ok=True)
    tmp = tempfile.mkdtemp(prefix="loop-")
    binary = os.path.join(tmp, "refactor")
    probe.sh(["go", "build", "-o", binary, "./cmd/refactor"])
    tools = compact(probe.tool_list()) if a.reference == "compact" else probe.schemas()
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
                row.update(attempt(t, model, tools, binary, a.repairs, log, may_read=not a.no_read, stages=a.stages))
                row.update(cost=model.cost, requests=model.requests, usage=model.usage)
                with open(os.path.join(a.out, f"{t['name']}-{m}-{n}.txt"), "w") as f:
                    f.write("\n\n==== next answer ====\n\n".join(log))
                results.write(json.dumps(row) + "\n")
                results.flush()
                shutil.rmtree(empty)
                print(json.dumps({k: row[k] for k in ("task", "model", "run", "passed", "requests", "repairs", "stages", "reads", "cost")}), flush=True)
    shutil.rmtree(tmp)


if __name__ == "__main__":
    main()
