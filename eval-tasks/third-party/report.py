#!/usr/bin/env python3
"""Merge candidates.py and verify.py output into the list and its tables.

    report.py CANDIDATES.jsonl VERIFIED.jsonl... --out candidates.jsonl > tables.md

Writes every candidate, with its verification if it has one, to --out, and
prints a Markdown table per repository of the verified feature candidates.
For a candidate with black-box packages it also gives the hidden check: the
PR's test files in those packages (the files a task's "tests" directory
holds) and the go test command that runs the tests the PR added or changed
there.
"""

import argparse
import json
import os
import sys

LAZYGIT_TESTS = "pkg/integration/tests/"


def check_for(c, v):
    files, cmds = [], []
    for pkg in v.get("black_box", []):
        if pkg == "integration":
            files += [f for f in c["test_files"] if f.startswith(LAZYGIT_TESTS)]
            for name in v["integration_tests"]:
                parts = "/".join("^" + p + "$" for p in name.split("/"))
                cmds.append(f"go test -count=1 -run '^TestIntegration$/{parts}' ./pkg/integration/clients/")
            continue
        files += [f for f in c["test_files"] if os.path.dirname(f) == pkg and f.endswith("_test.go")]
        names = v["go_tests"].get(pkg, [])
        cmds.append(f"go test -count=1 -run '^({'|'.join(names)})$' ./{pkg}")
    return {"files": sorted(set(files)), "command": " && ".join(cmds)} if files else None


def name_of(m):
    """Renders one of verify.py's missing names: "X", "Field Type" or
    "not enough F"."""
    parts = m.split()
    if parts[:2] in (["not", "enough"], ["too", "many"]):
        return f"{parts[-1]} (arguments)"
    if len(parts) == 2:
        return f"{parts[1]}.{parts[0]}"
    return m


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("candidates")
    ap.add_argument("verified", nargs="*")
    ap.add_argument("--out", required=True)
    args = ap.parse_args()
    verified = {}
    for path in args.verified:
        with open(path) as f:
            for line in f:
                v = json.loads(line)
                verified[(v["repo"], v["commit"])] = v
    rows = []
    with open(args.candidates) as f, open(args.out, "w") as out:
        for line in f:
            c = json.loads(line)
            v = verified.get((c["repo"], c["commit"]))
            if v:
                c["verification"] = {k: v[k] for k in (
                    "outcome", "black_box", "needs_api", "not_f2p", "failing_after", "missing",
                    "go_tests", "integration_tests", "why") if k in v}
                check = check_for(c, v)
                if check:
                    c["hidden_check"] = check
                bb, api = len(v.get("black_box", [])), len(v.get("needs_api", []))
                # Recommended: the black-box packages are at least half of
                # those whose tests check the PR, so the hidden check covers
                # most of what the PR's own tests do.
                c["recommended"] = c["kind"] == "feature" and v["outcome"] == "black_box" and bb >= api
                if c["kind"] == "feature":
                    rows.append(c)
            json.dump(c, out)
            out.write("\n")

    order = {"black_box": 0, "needs_api": 1, "not_f2p": 2, "broken": 3}
    by_repo = {}
    for c in rows:
        by_repo.setdefault(c["repo"], []).append(c)
    print("| Repository | Verified | Black-box | Recommended | Needs API | Not fail-to-pass | Broken |")
    print("|---|---|---|---|---|---|---|")
    for repo, cs in by_repo.items():
        n = lambda o: sum(c["verification"]["outcome"] == o for c in cs)
        print(f"| {repo} | {len(cs)} | {n('black_box')} | {sum(c['recommended'] for c in cs)} | {n('needs_api')} | {n('not_f2p')} | {n('broken')} |")
    print()
    for repo, cs in by_repo.items():
        cs.sort(key=lambda c: (not c["recommended"], order[c["verification"]["outcome"]], "SML".index(c["size"]), c["date"]))
        print(f"### {repo}\n")
        print("| PR | Issue | Title | Size (packages) | Refactored on the way | Outcome | Black-box test packages | Names a needs_api prompt must give |")
        print("|---|---|---|---|---|---|---|---|")
        for c in cs:
            v = c["verification"]
            if c["pr"]:
                pr = f"[#{c['pr']}](https://github.com/{repo}/pull/{c['pr']})"
            else:
                pr = f"[`{c['commit'][:9]}`](https://github.com/{repo}/commit/{c['commit']})"
            issues = ", ".join(f"[#{n}](https://github.com/{repo}/issues/{n})" for n in c.get("issues", [])[:2])
            sigs = list(dict.fromkeys(f"`{s['func']}` signature ({s['call_files']} calling files)" for s in c["signature_changes"]))
            moves = list(dict.fromkeys(f"moved `{m}`" for m in c["moved_funcs"]))
            refac = ", ".join(sigs[:2] + moves[:2])
            bb = len(v.get("black_box", []))
            api = len(v.get("needs_api", []))
            pk = f"{bb} of {bb + api + len(v.get('not_f2p', []))}" if bb else ""
            missing = ""
            if v["outcome"] == "needs_api":
                names = [name_of(m) for m in v.get("missing") or [] if m not in ("bool", "int", "string")]
                missing = ", ".join(f"`{n}`" for n in names[:4]) + (" ..." if len(names) > 4 else "")
            elif v["outcome"] == "broken":
                missing = v.get("why", "")
            title = c["title"].replace("|", "\\|")
            if len(title) > 70:
                title = title[:67] + "..."
            outcome = "**recommended**" if c["recommended"] else v["outcome"]
            print(f"| {pr} | {issues} | {title} | {c['size']} ({len(c['packages'])}) | {refac} | {outcome} | {pk} | {missing} |")
        print()


if __name__ == "__main__":
    main()
