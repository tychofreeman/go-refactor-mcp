"""Where an A/B run's tokens and cost go.

Input tokens: the first request's context (system prompt, tool
definitions, CLAUDE.md, the task), sent again on every request, and
what the conversation adds, by kind, each block counted once per request
it stays in context (chars/4, which undercounts code). Cost: the
session's cache reads, cache writes and output, priced by a least-squares
fit to the costs the runs report, and the cache writes split into the
first request's and the later ones'.

usage: python3 -I cost.py RUNDIR
"""
import glob
import json
import os
import sys
from collections import defaultdict

READ_BASH = ("cat", "sed", "head", "tail", "awk", "less", "nl")
SEARCH_BASH = ("grep", "rg", "ls", "find", "tree", "wc")

def kind_of(name, inp):
    n = name.replace("mcp__gorefactor__", "g:")
    if n in ("Read", "g:inspect"):
        return "reads of code"
    if n in ("Grep", "Glob"):
        return "searches"
    if n == "ToolSearch":
        return "loading tool schemas"
    if n == "Bash":
        cmd = inp.get("command", "").strip()
        first = cmd.split()[0] if cmd else ""
        if first == "cd" and "&&" in cmd:
            first = cmd.split("&&", 1)[1].split()[0]
        if first in READ_BASH and "<<" not in cmd and " > " not in cmd:
            return "reads of code"
        if first in SEARCH_BASH:
            return "searches"
        if first in ("go", "gofmt"):
            return "go test / build output"
        return "edits and other commands"
    if n == "g:check":
        return "go test / build output"
    return "edits and other commands"

def text(c):
    t = c.get("content")
    return t if isinstance(t, str) else " ".join(x.get("text", "") for x in t or [] if isinstance(x, dict))

tot = defaultdict(float)
usage = []        # per session: cache read, cache write, output, cost, first request's write
firstwrite = {}   # message id -> cache tokens it wrote
sessions = 0
for p in sorted(glob.glob(os.path.join(sys.argv[1], "*.jsonl"))):
    if p.endswith("results.jsonl") or os.path.getsize(p) == 0:
        continue
    reqs = {}      # message id -> context tokens of that request
    order = []
    blocks = []    # (request index it first appears before, kind, chars, part)
    uses = {}
    final = None
    for l in open(p):
        e = json.loads(l)
        if e.get("type") == "result":
            final = e
        m = e.get("message", {})
        if not isinstance(m, dict) or not isinstance(m.get("content"), list):
            continue
        if m.get("role") == "assistant":
            mid = m.get("id")
            if mid not in reqs:
                u = m.get("usage", {})
                reqs[mid] = u.get("input_tokens", 0) + u.get("cache_creation_input_tokens", 0) + u.get("cache_read_input_tokens", 0)
                firstwrite[mid] = u.get("cache_creation_input_tokens", 0)
                order.append(mid)
            at = len(order)   # visible from the next request on
            for c in m["content"]:
                if c.get("type") == "tool_use":
                    uses[c["id"]] = kind_of(c["name"], c["input"])
                    blocks.append((at, "the model's own calls and text", len(json.dumps(c["input"]))))
                elif c.get("type") in ("text", "thinking"):
                    blocks.append((at, "the model's own calls and text", len(c.get("text", "") or c.get("thinking", ""))))
        else:
            at = len(order)
            for c in m["content"]:
                if c.get("type") == "tool_result":
                    blocks.append((at, uses.get(c["tool_use_id"], "edits and other commands"), len(text(c))))
    if not order:
        continue
    sessions += 1
    n = len(order)
    fixed = reqs[order[0]]
    total = sum(reqs.values())
    tot["total"] += total
    tot["fixed"] += fixed * n
    for at, kind, chars in blocks:
        later = max(n - at, 0)
        tot[kind] += chars / 4 * later
    u = final.get("usage", {}) if final else {}
    first = order[0]
    fw = firstwrite.get(first, 0)
    usage.append((u.get("cache_read_input_tokens", 0), u.get("cache_creation_input_tokens", 0),
                  u.get("output_tokens", 0), final.get("total_cost_usd", 0) if final else 0, fw))
    tot["requests"] += n
    tot["usd"] += final.get("total_cost_usd", 0) if final else 0

T = tot["total"]
print(f"sessions {sessions}, requests {tot['requests']:.0f} ({tot['requests']/sessions:.1f} a session), input {T/sessions:,.0f} tokens a session")
print(f"first request's context (system prompt, tool definitions, CLAUDE.md, the task), on every request: {tot['fixed']/T:.1%}  ({tot['fixed']/tot['requests']:,.0f} tokens a request)")
known = 0
for k in ["the model's own calls and text", "reads of code", "searches", "go test / build output", "edits and other commands", "loading tool schemas"]:
    known += tot[k]
    print(f"  {k}: {tot[k]/T:.1%}")
print(f"  not in the transcript's text (schemas loaded by ToolSearch, framing): {(T - tot['fixed'] - known)/T:.1%}")


def solve(a, b):
    """The solution x of a x = b, by Gaussian elimination."""
    n = len(b)
    m = [a[i][:] + [b[i]] for i in range(n)]
    for i in range(n):
        p = max(range(i, n), key=lambda r: abs(m[r][i]))
        m[i], m[p] = m[p], m[i]
        for r in range(n):
            if r != i:
                f = m[r][i] / m[i][i]
                m[r] = [m[r][k] - f * m[i][k] for k in range(n + 1)]
    return [m[i][n] / m[i][i] for i in range(n)]


x = [[s[0] / 1e6, s[1] / 1e6, s[2] / 1e6] for s in usage]
y = [s[3] for s in usage]
price = solve([[sum(r[i] * r[j] for r in x) for j in range(3)] for i in range(3)],
              [sum(r[i] * c for r, c in zip(x, y)) for i in range(3)])
read, write, out = (sum(r[i] for r in x) * price[i] for i in range(3))
first = sum(s[4] for s in usage) / 1e6 * price[1]
total = read + write + out
print(f"fitted $ per million tokens: cache read {price[0]:.2f}, cache write {price[1]:.2f}, output {price[2]:.2f} "
      f"(fitted ${total:.2f}, reported ${sum(y):.2f})")
print(f"cost: cache writes {write/total:.0%} (the first request's {first/total:.0%}, later requests' {(write-first)/total:.0%}), "
      f"output {out/total:.0%}, cache reads {read/total:.0%}")
