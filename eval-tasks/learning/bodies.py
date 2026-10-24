"""Function bodies read in each session of an A/B run, by what the
session did with them: edited (its header or lines are replaced),
copied from (its lines reappear in code the session wrote), or unused
(nothing of it reaches an edit); unused split into bodies the read
asked for (by name or range) and bodies that came with a whole file.
Then what the reads and the unused bodies cost, each read counted
once per request it stays in context.

Edits are matched by their text (Edit, Write, Python and shell scripts,
gorefactor edits and batches), a heuristic: MINLEN (default 15) is the
shortest line of a body that counts as its own; 6 gives a looser bound.

usage: python3 -I bodies.py RUNDIR
"""
import glob
import json
import os
import re
import sys
from collections import defaultdict

FUNC = re.compile(r'^func (\(\s*\w*\s*\*?(\w+)(\[[^\]]*\])?\) )?(\w+)')
PREFIXES = [re.compile(r'^\s*\d+\t'), re.compile(r'^\s*\d+  ')]

def text(c):
    t = c.get("content")
    return t if isinstance(t, str) else " ".join(x.get("text", "") for x in t or [] if isinstance(x, dict))

def strip(line):
    for p in PREFIXES:
        m = p.match(line)
        if m:
            return line[m.end():], True
    return line, False

def bodies_in(result):
    """Function bodies shown in a read result: {name: (lines, chars, text)}."""
    out = {}
    lines = result.split("\n")
    i = 0
    while i < len(lines):
        s, _ = strip(lines[i])
        m = FUNC.match(s)
        if not m:
            i += 1
            continue
        name = (m.group(2) + "." if m.group(2) else "") + m.group(4)
        if s.rstrip().endswith("}"):            # one-line function: the signature shows it
            i += 1
            continue
        nxt = strip(lines[i + 1])[0] if i + 1 < len(lines) else ""
        if "… lines" in nxt:                    # inspect skeleton: body elided
            i += 2
            continue
        body = []
        j = i + 1
        while j < len(lines):
            t, _ = strip(lines[j])
            if t.startswith("}") or FUNC.match(t):
                break
            body.append(t)
            j += 1
        if body:
            prev = out.get(name)
            txt = "\n".join(body)
            if not prev or len(txt) > len(prev[2]):
                out[name] = (len(body), len(txt), txt)
        i = j + 1
    return out

READ_TOOLS = {"Read"}
def is_read(name, inp):
    n = name.replace("mcp__gorefactor__", "")
    if n == "inspect":
        return "inspect"
    if n == "Read":
        return "Read" if inp.get("file_path", "").endswith(".go") else None
    if n == "Bash":
        cmd = inp.get("command", "").strip()
        first = cmd.split()[0] if cmd else ""
        if first in ("cat", "sed", "head", "tail", "awk") and ".go" in cmd and ">" not in cmd.split("<<")[0]:
            return "Bash"
    return None

def edit_blobs(name, inp):
    """(old, any) text of an edit call: old is what it replaces where
    known (Edit's old_string), any is all of its input."""
    n = name.replace("mcp__gorefactor__", "")
    blob = json.dumps(inp)
    if n in ("Edit",):
        return inp.get("old_string", ""), blob
    if n == "MultiEdit":
        return "\n".join(e.get("old_string", "") for e in inp.get("edits", [])), blob
    if n in ("Write", "NotebookEdit"):
        return "", blob
    if n == "Bash":
        cmd = inp.get("command", "")
        if any(k in cmd for k in ("python", "cat >", "sed -i", "perl", "tee ", "> ")):
            return "", cmd
        return None
    if name.startswith("mcp__gorefactor__") and n not in ("inspect", "check", "undo"):
        return "", blob.replace("\\n", "\n").replace('\\t', '\t').replace('\\"', '"')
    return None

BORING = {"return nil", "return err", "return out", "return false", "return true", "default:", "continue", "break"}
def distinctive(txt):
    return [l.strip() for l in txt.split("\n") if len(l.strip()) >= int(os.environ.get("MINLEN", "15")) and l.strip() not in BORING]

def classify_bodies(seen, edits):
    """For each body read: "edited" (its header or lines are replaced),
    "copied" (its lines reappear in new code only), or "unused"."""
    out = {}
    for name, (_, _, txt) in seen.items():
        short = name.split(".")[-1]
        lines = distinctive(txt)
        verdict = "unused"
        for old, blob in edits:
            header = re.search(r"func (\([^)]*\) )?" + re.escape(short) + r"\b", blob) or \
                     re.search(r'"(function|name)":\s*"' + re.escape(name) + '"', blob)
            if header or (old and any(l in old for l in lines)):
                verdict = "edited"
                break
            if any(l in blob for l in lines):
                verdict = "copied"
        out[name] = verdict
    return out

def session(path):
    uses, results, order = {}, {}, []
    for l in open(path):
        e = json.loads(l)
        m = e.get("message", {})
        if not isinstance(m, dict) or not isinstance(m.get("content"), list):
            continue
        for c in m["content"]:
            if c.get("type") == "tool_use":
                uses[c["id"]] = c
                order.append(c["id"])
            elif c.get("type") == "tool_result":
                results[c["tool_use_id"]] = text(c)
    seen = {}          # body name -> (lines, chars, text), from any read
    how = {}           # body name -> first read kind and whether targeted
    reads = []
    edits = []
    for cid in order:
        c = uses[cid]
        kind = is_read(c["name"], c["input"])
        if kind:
            got = bodies_in(results.get(cid, ""))
            inp = c["input"]
            targeted = kind == "inspect" and (inp.get("function") or inp.get("targets") or inp.get("names") or inp.get("start_line")) or \
                       kind == "Read" and inp.get("offset") or kind == "Bash" and inp.get("command", "").startswith("sed -n")
            reads.append((kind, bool(targeted), set(got)))
            for k, v in got.items():
                if k not in seen or v[1] > seen[k][1]:
                    seen[k] = v
                how.setdefault(k, (kind, bool(targeted)))
        else:
            b = edit_blobs(c["name"], c["input"])
            if b:
                edits.append(b)
    return seen, how, reads, classify_bodies(seen, edits)

def chain_of(base):
    """The chain, arm and role of a session's transcript name."""
    chain = base.split("-")[1]
    arm = "step" if "-step" in base else "control"
    stage = int(re.search(r"#(\d+)", base).group(1)) if "#" in base else 1
    return chain, arm, "preparing" if arm == "step" and stage == 1 else "feature"


def context_cost(path, verdict):
    """Tokens the session's reads, and the unused bodies in them, cost:
    each read once per request from its own on."""
    msgs, uses, results = [], {}, {}
    for l in open(path):
        e = json.loads(l)
        m = e.get("message", {})
        if not isinstance(m, dict) or not isinstance(m.get("content"), list):
            continue
        if m.get("role") == "assistant" and m.get("id") not in msgs:
            msgs.append(m.get("id"))
        for c in m["content"]:
            if c.get("type") == "tool_use":
                uses[c["id"]] = (c, len(msgs))
            elif c.get("type") == "tool_result":
                results[c["tool_use_id"]] = text(c)
    read = unused = 0
    for cid, (c, at) in uses.items():
        if not is_read(c["name"], c["input"]):
            continue
        later = len(msgs) - at + 1
        got = bodies_in(results.get(cid, ""))
        read += len(results.get(cid, "")) / 4 * later
        unused += sum(v[1] for n, v in got.items() if verdict.get(n) == "unused") / 4 * later
    return read, unused


def main(d):
    inputs = {}
    for l in open(os.path.join(d, "results.jsonl")):
        r = json.loads(l)
        inputs[(r["task"], r["run"])] = r["input_tokens"]
    rows = defaultdict(lambda: defaultdict(float))
    for p in sorted(glob.glob(os.path.join(d, "*.jsonl"))):
        if p.endswith("results.jsonl") or os.path.getsize(p) == 0:
            continue
        base = os.path.basename(p)
        seen, how, reads, verdict = session(p)
        r = rows[chain_of(base)]
        r["sessions"] += 1
        r["reads"] += len(reads)
        for name, (_, ch, _) in seen.items():
            r["bodies"] += 1
            r["body_chars"] += ch
            v = verdict[name]
            if v == "unused":
                v = "unused_asked" if how[name][1] else "unused_file"
                r["unused_chars"] += ch
            r[v] += 1
        m = re.match(r"(.*)-\w+-(\d+)\.jsonl$", base)
        r["input"] += inputs.get((m.group(1), int(m.group(2))), 0)
        read, unused = context_cost(p, verdict)
        r["read_ctx"] += read
        r["unused_ctx"] += unused
    print("| chain | arm | session | sessions | reads | bodies read | edited | copied from | unused, asked for | unused, came with a file | unused share of body text | reads in context, share of input | unused bodies, share of input |")
    print("|---|---|---|---|---|---|---|---|---|---|---|---|---|")
    tot = defaultdict(float)
    for key in sorted(rows):
        r = rows[key]
        for k, v in r.items():
            tot[k] += v
        print(line(" | ".join(key), r))
    print(line("all | |", tot))


def line(key, r):
    n = r["sessions"]
    return (f"| {key} | {n:.0f} | {r['reads']/n:.1f} | {r['bodies']/n:.1f} | {r['edited']/n:.1f} | "
            f"{r['copied']/n:.1f} | {r['unused_asked']/n:.1f} | {r['unused_file']/n:.1f} | "
            f"{r['unused_chars']/max(r['body_chars'], 1):.0%} | {r['read_ctx']/max(r['input'], 1):.1%} | "
            f"{r['unused_ctx']/max(r['input'], 1):.1%} |")


main(sys.argv[1])
