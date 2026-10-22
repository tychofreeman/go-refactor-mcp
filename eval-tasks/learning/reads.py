"""Per-run reading measures over A/B transcripts: read calls, turns with a
read, read result tokens (chars/4), "where is X" searches, and bodies
read against bodies changed.

usage: reads.py FUNCS.jsonl RUNDIR[:TASKS] ...   (TASKS comma-separated)
FUNCS.jsonl: go run ./eval-tasks/learning/funcs eval-tasks/testdata/freight-xl
"""
import json
import re
import shlex
import sys
from collections import defaultdict

funcs = [json.loads(l) for l in open(sys.argv[1])]
by_file = defaultdict(list)
for f in funcs:
    by_file[f["file"]].append(f)


def rel(path):
    m = re.search(r"/tmp/ab-[^/]+/(.*)", path)
    return m.group(1) if m else path.lstrip("./")


def bodies(file, lo=None, hi=None):
    out = set()
    for f in by_file.get(rel(file), []):
        if lo is None or (f["body_start"] <= hi and f["end"] >= lo):
            out.add(f["file"] + ":" + f["name"])
    return out


def body_of(file, name):
    return {f["file"] + ":" + f["name"] for f in by_file.get(rel(file), []) if f["name"] == name}


def text(c):
    t = c.get("content")
    return t if isinstance(t, str) else " ".join(x.get("text", "") for x in t or [] if isinstance(x, dict))


def classify(name, inp):
    """Returns (kind, bodies) for a read call, or None."""
    name = name.replace("mcp__gorefactor__", "")
    if name == "inspect":
        if inp.get("targets"):
            got = set()
            for t in inp["targets"]:
                got |= classify("inspect", t)[1]
            return "inspect targets", got
        if not inp.get("file"):
            return "inspect overview", set()
        if inp.get("function"):
            return "inspect function", body_of(inp["file"], inp["function"])
        if inp.get("start_line"):
            return "inspect range", bodies(inp["file"], inp["start_line"], inp.get("end_line", inp["start_line"]))
        return "inspect skeleton", set()
    if name == "Read":
        off = inp.get("offset")
        if off:
            return "Read", bodies(inp["file_path"], off, off + inp.get("limit", 2000))
        return "Read", bodies(inp["file_path"])
    if name in ("Grep", "Glob"):
        return "search", set()
    if name == "Bash":
        cmd = inp.get("command", "")
        first = cmd.strip().split()[0] if cmd.strip() else ""
        if first in ("grep", "rg") or "| grep" in cmd and first in ("cat",):
            return "search", set()
        m = re.match(r"\s*sed -n ['\"]?(\d+),(\d+)p['\"]? (\S+)", cmd)
        if m:
            return "Bash range", bodies(m.group(3), int(m.group(1)), int(m.group(2)))
        if first in ("cat", "head", "tail", "less"):
            try:
                files = [a for a in shlex.split(cmd)[1:] if a.endswith(".go")]
            except ValueError:
                files = []
            got = set()
            for f in files:
                got |= bodies(f)
            return "Bash file", got
        if first in ("ls", "find", "tree"):
            return "search", set()
    return None


def run(path):
    turns = defaultdict(list)
    results = {}
    for line in open(path):
        e = json.loads(line)
        m = e.get("message", {})
        if not isinstance(m, dict) or not isinstance(m.get("content"), list):
            continue
        for c in m["content"]:
            if c.get("type") == "tool_use":
                turns[m.get("id")].append(c)
            elif c.get("type") == "tool_result":
                results[c["tool_use_id"]] = len(text(c))
    reads, read_turns, tokens, searches, read = 0, 0, 0, 0, set()
    kinds = defaultdict(int)
    for calls in turns.values():
        any_read = False
        for c in calls:
            k = classify(c["name"], c["input"])
            if not k:
                continue
            any_read = True
            reads += 1
            kinds[k[0]] += 1
            tokens += results.get(c["id"], 0) // 4
            if k[0] == "search":
                searches += 1
            read |= k[1]
        read_turns += any_read
    return {"reads": reads, "read_turns": read_turns, "turns": len(turns), "tokens": tokens,
            "searches": searches, "bodies": len(read), "kinds": dict(kinds)}


rows = []
for spec in sys.argv[2:]:
    d, _, tasks = spec.partition(":")
    res = {}
    for l in open(d + "/results.jsonl"):
        r = json.loads(l)
        res[(r["task"], r["run"])] = r
    for (task, n), r in sorted(res.items()):
        if tasks and task.replace("freight-", "") not in tasks.split(","):
            continue
        m = run(f"{d}/{task}-mcp-{n}.jsonl")
        m.update(arm=d.split("/")[-1], task=task.replace("freight-", ""), run=n,
                 changed=r.get("quality", {}).get("changed_funcs", 0), cost=r["cost_usd"])
        rows.append(m)

agg = defaultdict(list)
for m in rows:
    agg[(m["task"], m["arm"])].append(m)
print("| task | arm | runs | read calls | turns with a read / turns | read tokens | searches | bodies read | bodies changed |")
print("|---|---|---|---|---|---|---|---|---|")
for (task, arm), ms in sorted(agg.items()):
    n = len(ms)
    mean = lambda k: sum(x[k] for x in ms) / n
    print(f"| {task} | {arm} | {n} | {mean('reads'):.1f} | {mean('read_turns'):.1f} / {mean('turns'):.1f} | {mean('tokens'):.0f} | {mean('searches'):.1f} | {mean('bodies'):.1f} | {mean('changed'):.1f} |")
kinds = defaultdict(lambda: defaultdict(int))
for m in rows:
    for k, v in m["kinds"].items():
        kinds[m["arm"]][k] += v
print()
for arm, ks in kinds.items():
    print(arm, dict(ks))
