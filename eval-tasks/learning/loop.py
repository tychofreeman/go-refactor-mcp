"""Plan, then execute: a loop of our own (docs/plan-and-execute.md, step 1).

For each task, a fresh model with no tools gets the task, the tools' input
schemas and the call-tree view (as probe.py -schemas gives them), and
answers with the whole change as one gorefactor batch; or first asks to
read declarations, which it may also do in place of any later batch, at
most three times; it gets them as inspect shows them, with line numbers,
from the module as the kept batches left it. With -read-after it may read
only once a batch has applied, and is told so only then: the first batch
derives what moves by name, and reads come for the edits that remain. The batch runs on a
fresh copy of the module, then the module's own tests. A refusal or a
failing test goes back in the same conversation (claude -p --resume, so
the prefix stays cached) with the numbered source of each place it names,
for the batch again, at most -repairs times. When a batch applies and
the tests pass, the model is asked whether the change is done; if not,
its next batch runs on top, at most -stages more. With -arm manual, the
model answers with text edits ({"file", "old", "new"} or {"file",
"content"}) that the loop applies, without the tools reference: the
same loop, to compare deriving with writing and nothing else different. With -patch, a repair
lists the failed batch's steps and may answer with those from one on,
the ones before kept: a repair's output is most of what the loop costs. The task's hidden
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

# READ_AFTER, in PLAN with -read-after, asks for a first batch that
# leaves the tests passing, without saying that reads come after it
# (said, the plans left the move for after reading the code).
READ_AFTER = """
Every line of the finished change is either code already in the module,
in a new place, or code that is new. Your first batch puts the existing
code in its new place with steps that address it by name (they carry
code you cannot see), and leaves the module's tests passing. Write no
code in it that the module already has somewhere. Once it applies, you
are asked for the next batch, for what is new."""

# READ_OPEN ends the later prompts with -read-after: the first word of
# reads, so the first batch is planned without knowing they come.
READ_OPEN = """
You may now also ask for source, with line numbers, at most three times:
{"read": ["Name", "Type.Method" or "pkg.Name", ...]}."""

# NOT_YET answers a read asked for before a batch applied, with -read-after.
NOT_YET = """Source comes once a batch has applied. Answer first with a batch
that derives what it can by name and leaves the module's tests passing:
{"batch": {"steps": [...]}}."""

# READ_HINT ends the later prompts when the model may read.
READ_HINT = """
Or, to see source first: {"read": ["Name", ...]}."""

REPAIR = """The batch did not succeed:

{what}

{source}Nothing of it was kept: the next batch runs on the module as it was
before this one. Answer with that whole batch again, corrected:
{{"batch": {{"steps": [...]}}}}."""


# PATCH, with -patch, asks a repair for the steps from one on, those
# before it kept, in place of the whole batch again: a repair's output
# is most of what the loop costs.
PATCH = """The batch did not succeed:

{what}

{source}Nothing of it was kept: the next batch runs on the module as it was
before this one. Its steps were:

{listing}

Answer with the steps that replace it from one of them on, the ones
before kept as they were (line numbers still as when the batch began):
{{"patch": {{"from": N, "steps": [...]}}}}{suggest}
or with the whole batch again: {{"batch": {{"steps": [...]}}}}."""


def listing(steps):
    """The steps, numbered, each with its tool and its arguments cut short
    (or, an edit of the manual arm, its file and what it replaces)."""
    short = lambda v: v if not isinstance(v, str) or len(v) <= 40 else v[:37] + "..."
    out = []
    for i, s in enumerate(steps, 1):
        if not isinstance(s, dict):
            out.append(f"{i:3}  {short(json.dumps(s))}")
        elif "tool" in s:
            args = {k: short(v) for k, v in (s.get("args") or {}).items()}
            out.append(f"{i:3}  {s.get('tool')}  {json.dumps(args)[:140]}")
        else:
            what = "content" if "content" in s else "old: " + json.dumps(short(s.get("old")))
            out.append(f"{i:3}  {s.get('file')}  {what}")
    return "\n".join(out)


def suggestion(what, n, unit="step"):
    """Which from fits the failure what, of a batch of n steps (or edits)."""
    m = re.match(r"REFUSED \w+ at (?:step|edit) (\d+) of", what)
    if m:
        return f"\n(from {m.group(1)} redoes the refused {unit} and what follows it; from {n + 1} adds {unit}s after them all)"
    return f"\n(from {n + 1} adds {unit}s after them all)"


def patched(last, patch):
    """The batch last with its steps from patch["from"] on replaced by
    patch["steps"] (the manual arm's patch["edits"]), or None if patch is
    not one."""
    if isinstance(patch, dict) and "steps" not in patch and isinstance(patch.get("edits"), list):
        patch = dict(patch, steps=patch["edits"])
    if not isinstance(patch, dict) or not isinstance(patch.get("steps"), list):
        return None
    try:
        k = int(patch.get("from", 1))
    except (TypeError, ValueError):
        return None
    k = min(max(k, 1), len(last["steps"]) + 1)
    return dict(last, steps=last["steps"][:k - 1] + patch["steps"])


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


# The manual arm (-arm manual): the same loop, but the model answers
# with text edits, which the loop applies, in place of gorefactor steps.
PLAN_MANUAL = """You are making a change to a Go module. You have no tools in this
conversation: you answer with JSON, and a program applies it and tells
you what happened.

The module, as a call tree (names, signatures and doc comments; no bodies):

{view}

The change:
{prompt}

Answer with one JSON object and nothing else:
{{"edits": [the whole change as edits, in order]}}
where an edit is {{"file": PATH, "old": TEXT, "new": TEXT}}, which
replaces the one occurrence of old in the file as the edits before it
left it (old exactly as in the file, its indentation included, without
line numbers), or {{"file": PATH, "content": TEXT}}, which writes a
whole file.{read}
After the edits, the module's own tests run; you get one message back
with what failed, if anything."""

MANUAL_READ = """
Or, to see code first: {"read": ["Name", "Type.Method" or "pkg.Name", ...]}
for the source of functions, methods, types and variables, tests
included, with line numbers. You may answer with a read in place of
edits, now or after any later message, at most three times in all."""

READ_MANUAL = """The source you asked for, from the module as it is now, with its
line numbers:

{source}

Answer with the edits: {{"edits": [...]}}."""

DONE_MANUAL = """The edits applied, and the module's own tests pass. If that
completes the change asked for, answer {"done": true}. If not, answer
with the next edits, which apply to the module as these left it:
{"edits": [...]}."""

UNPARSED_MANUAL = """Your answer was not one JSON object with an "edits" key that parses.
Answer with that JSON alone, nothing before or after it:
{"edits": [...]}."""

REPAIR_MANUAL = """The edits did not succeed:

{what}

{source}Nothing of them was kept: the next edits apply to the module as it
was before these. Answer with all of them again, corrected:
{{"edits": [...]}}."""

PATCH_MANUAL = """The edits did not succeed:

{what}

{source}Nothing of them was kept: the next edits apply to the module as it
was before these. They were:

{listing}

Answer with the edits that replace them from one of them on, the ones
before kept as they were:
{{"patch": {{"from": N, "edits": [...]}}}}{suggest}
or with all of them again: {{"edits": [...]}}."""


def run_edits(binary, module, batch):
    """Applies the edits of batch["steps"], in order, to a fresh copy of
    module, then runs go vet and the module's own tests, as run_batch
    does. After an edit that cannot apply, the copy has those before it."""
    d = probe.copy_module(module)
    edits = batch["steps"]
    for i, e in enumerate(edits, 1):
        if why := apply_edit(d, e):
            return d, False, f"REFUSED edits at edit {i} of {len(edits)}: {why}"
    t = probe.sh(["sh", "-c", "go vet ./... && go test ./..."], cwd=d)
    if t.returncode != 0:
        return d, False, "The edits applied, but the module's tests failed:\n" + (t.stdout + t.stderr)[:3000]
    return d, True, ""


def apply_edit(d, e):
    """Applies the edit e in d, or says why it cannot."""
    if not isinstance(e, dict) or not isinstance(e.get("file"), str):
        return 'an edit is {"file": PATH, "old": TEXT, "new": TEXT} or {"file": PATH, "content": TEXT}'
    path = os.path.normpath(os.path.join(d, e["file"]))
    if not path.startswith(d + os.sep):
        return f"{e['file']} is outside the module"
    if isinstance(e.get("content"), str):
        os.makedirs(os.path.dirname(path), exist_ok=True)
        open(path, "w").write(e["content"])
        return ""
    if not isinstance(e.get("old"), str) or not isinstance(e.get("new"), str):
        return f"{e['file']}: give old and new, or content"
    if not os.path.isfile(path):
        return f"{e['file']} does not exist; give content to write a new file"
    src = open(path).read()
    n = src.count(e["old"]) if e["old"] else 0
    if n != 1:
        return f"{e['file']}: old occurs {n} times; it must occur exactly once, as in the file (indentation included, no line numbers)"
    open(path, "w").write(src.replace(e["old"], e["new"], 1))
    return ""


class Arm:
    """What the model answers with and how the loop runs it: gorefactor
    steps (the refactor arm) or text edits (the manual arm)."""

    def __init__(self, name, plan, may_read, read, done, unparsed, repair, patch, run, unit):
        self.name, self.plan, self.may_read, self.read, self.done = name, plan, may_read, read, done
        self.unparsed, self.repair, self.patch, self.run, self.unit = unparsed, repair, patch, run, unit

    def batch(self, ans):
        """The batch an answer gives, {"steps": [...]} and its options, or None."""
        if not ans:
            return None
        if self.name == "manual":
            return {"steps": ans["edits"]} if isinstance(ans.get("edits"), list) else None
        return ans["batch"] if isinstance(ans.get("batch"), dict) else None


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


ARMS = {
    "refactor": Arm("refactor", PLAN, MAY_READ, READ, DONE, UNPARSED, REPAIR, PATCH, run_batch, "step"),
    "manual": Arm("manual", PLAN_MANUAL, MANUAL_READ, READ_MANUAL, DONE_MANUAL, UNPARSED_MANUAL, REPAIR_MANUAL, PATCH_MANUAL, run_edits, "edit"),
}


def attempt(task, model, tools, binary, repairs, log, may_read=True, stages=2, read_after=False, patch=False, arm=ARMS["refactor"]):
    """One attempt at task. A batch that applies and passes the module's
    tests is kept, and the model is asked whether the change is done; if
    not, its next batch runs on what the kept ones left, at most stages
    more. A refusal or a failing test gets a repair, at most repairs in
    all; the next batch then runs where the failed one began. With
    read_after, reads open only once a batch has applied; with patch, a
    repair may answer with the failed batch's steps from one on. arm says
    what the model answers with: steps, or text edits (a batch's steps
    then being the edits)."""
    module = os.path.join(ROOT, "eval-tasks", task["dir"])
    text = arm.plan.format(tools=tools, view=probe.view("calltree", module), prompt=task["prompt"],
                           read=READ_AFTER if read_after else arm.may_read if may_read else "")
    may_read = may_read and not read_after
    row = {"reads": 0, "repairs": 0, "patches": 0, "stages": 0, "refusals": [], "error": ""}
    base, batches, left, reads = module, [], repairs, 3  # base: where the next batch runs
    last = None  # the batch that failed last, which a patch changes
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
            ask(arm.read.format(source=source(binary, base, ans["read"])))
            continue
        if read_after and reads and not batches and ans and isinstance(ans.get("read"), list) and not ans.get("batch"):
            reads -= 1
            row["refusals"].append("read before a batch applied")
            ask(NOT_YET)
            continue
        batch = arm.batch(ans)
        if patch and last and ans and batch is None and (b := patched(last, ans.get("patch"))):
            batch = b
            row["patches"] += 1
        if batch is None:
            row["refusals"].append(err or "no batch in the answer")
            if left == 0:
                row["error"] = err or "no batch in the answer"
                break
            left -= 1
            row["repairs"] += 1
            ask(arm.unparsed)
            continue
        last = batch
        d, ok, what = arm.run(binary, base, last)
        if ok:
            last = None
            batches.append(batch)
            row["stages"] += 1
            if base != module:
                shutil.rmtree(base)
            base = d
            if row["stages"] > stages:
                break
            if read_after:
                may_read, hint = True, READ_OPEN
            ask(arm.done + hint)
            continue
        row["refusals"].append(what.splitlines()[0][:200])
        if left == 0:
            shutil.rmtree(d)
            break
        left -= 1
        row["repairs"] += 1
        places = named_places(d, what)
        shutil.rmtree(d)
        if patch and isinstance(last.get("steps"), list):
            ask(arm.patch.format(what=what, source=places, listing=listing(last["steps"]), suggest=suggestion(what, len(last["steps"]), arm.unit)) + hint)
        else:
            ask(arm.repair.format(what=what, source=places) + hint)
    row["applied"] = bool(batches)
    row["passed"] = bool(batches) and hidden(base, task["tests"])
    if base != module:
        shutil.rmtree(base)
    every = {"steps": [s for b in batches for s in b.get("steps", [])]}
    if arm.name == "manual":  # an edit's new or content is what it writes
        row.update(steps=len(every["steps"]), written=probe.written({"steps": [{"args": e} for e in every["steps"] if isinstance(e, dict)]}), tools=["edits"])
    else:
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
    p.add_argument("-read-after", action="store_true", help="the plan may ask for source only once a batch has applied")
    p.add_argument("-patch", action="store_true", help="a repair may answer with the failed batch's steps from one on, the ones before kept")
    p.add_argument("-arm", default="refactor", choices=sorted(ARMS),
                   help="what the model answers with: gorefactor steps, or text edits the loop applies")
    p.add_argument("-reference", default="compact", choices=["compact", "full"],
                   help="the tools as compact signatures and descriptions, or as the server publishes them")
    p.add_argument("-stages", type=int, default=2, help="batches after the first that the model may send when asked if it is done")
    a = p.parse_args()
    if a.arm == "manual" and (a.read_after or "ref" in a.models.split(",")):
        p.error("the manual arm has no reference edits, and -read-after asks for a first batch that moves code by name")
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
                row = {"task": t["name"], "model": m, "run": n, "arm": a.arm}
                row.update(attempt(t, model, tools, binary, a.repairs, log, may_read=not a.no_read, stages=a.stages, read_after=a.read_after, patch=a.patch, arm=ARMS[a.arm]))
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
