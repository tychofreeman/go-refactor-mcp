"""The call-tree probe (docs/reading-and-deriving.md, step 2).

For each task of a task file and each view of its module (the call tree,
the inspect skeleton of every file, or the whole module), a fresh model
with no tools gets the task's prompt, the tool descriptions and the view (in that order, so that questions share a cached prefix),
and answers in one turn: A, the functions whose code must change, and B,
the change as a gorefactor batch. A is scored against the functions the
reference solution (eval-tasks/learning/refs/<task>.json) changes; B by
running it on a fresh copy and then the task's hidden tests.

Run from the repository root:

    python3 eval-tasks/learning/probe.py -tasks eval-tasks/derivations-freight.json \
        -only freight-dispatch,freight-run -runs 4 -out eval-runs/calltree-probe

-model picks the model (default sonnet); -schemas gives the tools' input
schemas, as the server publishes them, in place of the CLI's descriptions;
-plan also asks for the derivation in words, one entry per step, which is
kept with the answer, and the tools the batch uses are compared with the
reference's.

Needs the claude CLI; it costs a few cents a question.
"""
import argparse
import json
import os
import re
import shutil
import subprocess
import tempfile

ROOT = os.getcwd()
VIEWS = ("calltree", "skeleton", "whole")

QUESTION = """You are planning a change to a Go module. You cannot read anything
else and have no tools in this turn: answer from what is below.

The tools you would make it with (gorefactor; each step of a batch is
{{"tool": NAME, "args": {{...}}}}, with the arguments that tool takes):

{tools}

The module ({view_name}):

{view}

The change:
{prompt}

Answer with one JSON object and nothing else:
{{{plan}"where": [the existing functions and methods whose code must change, each
as "pkg.Name" or "pkg.Type.Method", pkg being the package's name],
 "batch": {{"steps": [the change as gorefactor steps, in order; a line of Go
you write yourself goes in an edit step (add_declaration,
replace_statements, insert_statements, replace_expression, ...)]}}}}
Line numbers you do not know: address declarations by name where a tool
allows it."""


PLAN = """"plan": [the derivation in words, one entry per step: the operation
(a refactoring, or an edit you write) and what it applies to, as you would
explain the change to a colleague before touching the code],
 """


def schemas():
    """The server's tools as it publishes them: each one's name,
    description and input schema."""
    d = tempfile.mkdtemp(prefix="probe-mcp-")
    binary = os.path.join(d, "refactor-mcp")
    sh(["go", "build", "-o", binary, "./cmd/refactor-mcp"])
    p = subprocess.Popen([binary], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, cwd=d)

    def rpc(i, method, params):
        p.stdin.write(json.dumps({"jsonrpc": "2.0", "id": i, "method": method, "params": params}) + "\n")
        p.stdin.flush()
        while True:
            m = json.loads(p.stdout.readline())
            if m.get("id") == i:
                return m["result"]

    rpc(1, "initialize", {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "probe", "version": "0"}})
    p.stdin.write(json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized"}) + "\n")
    p.stdin.flush()
    tools = rpc(2, "tools/list", {})["tools"]
    p.kill()
    shutil.rmtree(d)
    return "\n\n".join(f"{t['name']}: {t['description']}\ninput schema: {json.dumps(t['inputSchema'])}" for t in tools)


def tools_of(batch):
    """The tools a batch's steps use, in order, without repeats."""
    out = []
    for s in batch.get("steps", []):
        if isinstance(s, dict) and s.get("tool") not in out:
            out.append(s.get("tool"))
    return out


def sh(cmd, cwd=ROOT, stdin=None):
    return subprocess.run(cmd, cwd=cwd, input=stdin, capture_output=True, text=True)


def view(kind, module):
    if kind == "calltree":
        files = sorted(os.path.relpath(os.path.join(d, f), module) for d, _, fs in os.walk(module) for f in fs if f.endswith(".go"))
        return "Files: " + ", ".join(files) + "\n\n" + sh(["go", "run", "./cmd/calltree", module]).stdout
    return sh(["go", "run", "./eval-tasks/learning/views", kind, module]).stdout


def funcs(module):
    out = sh(["go", "run", "./eval-tasks/learning/funcs", module]).stdout
    return [json.loads(l) for l in out.splitlines()]


def bodies(module):
    """Maps "pkg.Name" to the source of each function of module."""
    out = {}
    for f in funcs(module):
        lines = open(os.path.join(module, f["file"])).read().splitlines()
        pkg = os.path.basename(os.path.dirname(f["file"]))
        out[pkg + "." + f["name"]] = "\n".join(lines[f["start"] - 1:f["end"]])
    return out


def copy_module(module):
    d = tempfile.mkdtemp(prefix="probe-")
    shutil.copytree(module, d, dirs_exist_ok=True)
    return d


def apply(module, batch, tests):
    """Runs batch on a copy of module; returns (copy, applied, refusal,
    passed)."""
    d = copy_module(module)
    path = os.path.join(d, ".batch.json")
    json.dump(batch, open(path, "w"))
    r = sh(["go", "run", "./cmd/refactor", "-C", d, "batch", "@" + path])
    os.remove(path)
    out = r.stdout + r.stderr
    applied = out.lstrip().startswith("APPLIED")
    refusal = "" if applied else out.strip().splitlines()[0][:300] if out.strip() else "no output"
    passed = False
    if applied:
        shutil.copytree(os.path.join(ROOT, "eval-tasks", tests), d, dirs_exist_ok=True)
        passed = sh(["sh", "-c", "go vet ./... && go test ./..."], cwd=d).returncode == 0
    return d, applied, refusal, passed


def changed(module, after):
    before, now = bodies(module), bodies(after)
    return {k for k, v in before.items() if now.get(k) != v}


EDIT_FIELDS = ("content", "new", "field", "element", "value", "type")


def written(batch):
    """The distinct non-blank lines of Go the batch's edit steps write."""
    lines = set()
    for s in batch.get("steps", []):
        for k in EDIT_FIELDS:
            v = s.get("args", {}).get(k)
            if isinstance(v, str):
                lines |= {l.strip() for l in v.splitlines() if l.strip()}
    return len(lines)


def ask(text, cwd, model):
    r = sh(["claude", "-p", "--model", model, "--output-format", "json"], cwd=cwd, stdin=text)
    try:
        res = json.loads(r.stdout)
    except json.JSONDecodeError:
        return None, 0.0, r.stdout[:300] + r.stderr[:300]
    return res.get("result", ""), res.get("total_cost_usd", 0.0), ""


def parse(answer):
    m = re.search(r"\{.*\}", answer or "", re.S)
    if not m:
        return None
    try:
        return json.loads(m.group(0))
    except json.JSONDecodeError:
        return None


def main():
    p = argparse.ArgumentParser()
    p.add_argument("-tasks", required=True)
    p.add_argument("-only", default="")
    p.add_argument("-views", default=",".join(VIEWS))
    p.add_argument("-runs", type=int, default=4)
    p.add_argument("-out", required=True)
    p.add_argument("-model", default="sonnet")
    p.add_argument("-schemas", action="store_true")
    p.add_argument("-plan", action="store_true")
    a = p.parse_args()
    os.makedirs(a.out, exist_ok=True)
    tools = schemas() if a.schemas else sh(["go", "run", "./cmd/refactor", "list"]).stdout
    tasks = [t for t in json.load(open(a.tasks)) if not a.only or t["name"] in a.only.split(",")]
    results = open(os.path.join(a.out, "results.jsonl"), "a")
    empty = tempfile.mkdtemp(prefix="probe-empty-")
    for t in tasks:
        module = os.path.join(ROOT, "eval-tasks", t["dir"])
        ref = json.load(open(os.path.join(ROOT, "eval-tasks/learning/refs", t["name"] + ".json")))
        d, ok, why, passed = apply(module, ref, t["tests"])
        want = changed(module, d)
        shutil.rmtree(d)
        if not (ok and passed):
            raise SystemExit(f"{t['name']}: the reference does not pass ({why})")
        for v in a.views.split(","):
            text = QUESTION.format(prompt=t["prompt"], tools=tools, view_name=v, view=view(v, module),
                                   plan=PLAN if a.plan else "")
            for n in range(1, a.runs + 1):
                answer, cost, err = ask(text, empty, a.model)
                ans = parse(answer)
                row = {"task": t["name"], "view": v, "run": n, "model": a.model, "cost": cost,
                       "prompt_chars": len(text), "error": err}
                if ans is None:
                    row.update(parsed=False)
                else:
                    where = set(ans.get("where") or [])
                    hit = where & want
                    batch = ans.get("batch") or {}
                    d, ok, why, passed = apply(module, batch, t["tests"])
                    shutil.rmtree(d)
                    row.update(parsed=True, where=sorted(where), precision=len(hit) / len(where) if where else 0.0,
                               recall=len(hit) / len(want) if want else 0.0, applied=ok, refusal=why,
                               passed=passed, steps=len(batch.get("steps", [])), written=written(batch),
                               plan=ans.get("plan"), tools=tools_of(batch), ref_tools=tools_of(ref))
                with open(os.path.join(a.out, f"{t['name']}-{v}-{a.model}-{n}.txt"), "w") as f:
                    f.write(answer or err)
                results.write(json.dumps(row) + "\n")
                results.flush()
                print(json.dumps({k: row.get(k) for k in ("task", "view", "run", "cost", "precision", "recall", "applied", "passed", "written")}), flush=True)


if __name__ == "__main__":
    main()
