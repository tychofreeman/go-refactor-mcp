#!/usr/bin/env python3
"""List candidate feature PRs of a Go repository for the A/B harness.

    candidates.py REPO_DIR NAME [--since 2026-07-01]

REPO_DIR is a clone with history back past --since. A candidate is a
first-parent commit (a merged PR) after --since whose title does not mark
it as a fix or maintenance, that changes non-test Go code in at least two
packages and changes tests. For each it prints one JSON object per line:
its commit and parent, PR number, the issues it closes, title, whether it
is a feature or internal work, the size of its non-test Go
change, its test files, the other files it changes, and whether it
refactored on the way (a function's signature changed and its calls in
other files changed with it, or a function moved between files).
"""

import argparse
import collections
import json
import os
import re
import subprocess
import sys

SKIP_TITLE = re.compile(
    r"^(\[[^\]]*\]\s*)?("
    r"(fix|bug|chore|ci|build|docs?|test|tests|refactor|perf|style|deps|revert|lint)(\([^)]*\))?!?:"
    r"|[Ff]ix(es|ed)?\b|[Rr]evert|[Bb]ump |[Uu]pdate (dep|module|go)|[Uu]pgrade to Go|Merge (branch|commit)"
    r"|[Dd]elete |[Rr]emove |[Rr]etire |[Dd]eprecate)"
)
# Titles of changes with no behavior a prompt could ask for: performance,
# code clean-ups, internal reworks and scaffolding, test and tooling work,
# hardening. They stay in the list, marked "internal".
INTERNAL = re.compile(
    r"go fix|modernize|^Improve performance|[Ss]peed up|^Avoid|[Ii]solate tests|race detection|^Merge v\d"
    r"|small fixes|[Rr]e-?work|^Alternate|^Refactor|[Uu]pdate .*tests|^Use fmt\.|[Pp]lumb|[Ss]tub out"
    r"|[Ff]inish up|^Route .* through|review feedback|^Finalize|UI polish|[Cc]onfine|^Validate|[Ee]scape"
    r"|[Ss]ynchroni[sz]e|UI thread|startup time|^Prepare|[Cc]onsolidate|Remove \"Maybe\"|Segregate"
    r"|Decompact|correct parser|\.Derive|^Experiment|[Ss]keleton|shared representation|[Ff]irst pass"
    r"|Change the way|minimum git version|tls defaults|Replace SSE|^Replace |^Use API|[Ss]tack \d+/\d+"
    r"|tests from a tarball|^Move Github PR cache|^Add a skill|[Ss]kill (agents|host)|recording"
    r"|[Tt]elemetry|[Ll]uminance|[Ee]-mail templates|header \(#|optimi[sz]e"
)
ISSUE = re.compile(r"\b(?:[Ff]ix(?:es|ed)?|[Cc]loses?|[Rr]esolves?|[Uu]pdates?|[Pp]art of|[Rr]efs?)[: ]+#(\d+)")
SKIP_PATH = re.compile(r"(^|/)(vendor|testdata|third_party)/")
# Test code that is not in _test.go files: lazygit's integration tests are
# ordinary Go files registered in tests/test_list.go.
TEST_PATH = re.compile(r"(_test\.go$|^pkg/integration/tests/)")
GENERATED = re.compile(r"(\.pb(\.gw)?\.go|_gen(erated)?\.go|zz_generated[^/]*\.go|_string\.go)$")
FUNC = re.compile(r"^func\s+(\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*(\[[^\]]*\])?\s*\(")


def git(repo, *args):
    return subprocess.run(["git", "-C", repo, *args], check=True, capture_output=True, text=True).stdout


def title_of(subject, body):
    if subject.startswith("Merge pull request"):
        m = re.search(r"#(\d+)", subject)
        first = body.strip().splitlines()[0] if body.strip() else subject
        return first, m and int(m.group(1))
    m = re.search(r"\(#(\d+)\)\s*$", subject)
    return subject, m and int(m.group(1))


def refactor_signals(repo, parent, commit, go_files):
    """Counts functions whose signature changed with calls changed in other
    files, and functions that moved from one file to another."""
    diff = git(repo, "diff", "-U0", parent, commit, "--", *go_files) if go_files else ""
    removed, added = {}, {}  # name -> (file, signature line)
    plus_lines = collections.defaultdict(list)  # file -> added lines
    path = None
    for line in diff.splitlines():
        if line.startswith("+++ "):
            path = line[6:] if line.startswith("+++ b/") else None
            continue
        if line.startswith("--- "):
            old = line[6:] if line.startswith("--- a/") else None
            path = path or old
            continue
        if line.startswith("diff --git"):
            m = re.match(r"diff --git a/(\S+) b/(\S+)", line)
            path = m.group(2) if m else None
            continue
        if not path or line.startswith("@@"):
            continue
        sign, text = line[:1], line[1:]
        m = FUNC.match(text)
        if m:
            key = (m.group(1) or "").strip(), m.group(2)
            (removed if sign == "-" else added if sign == "+" else {})[key] = (path, text.strip())
        if sign == "+":
            plus_lines[path].append(text)
    signature_changes, moves = [], []
    for key, (rfile, rsig) in removed.items():
        if key not in added:
            continue
        afile, asig = added[key]
        name = key[1]
        if afile != rfile:
            moves.append(name)
            continue
        if rsig == asig:
            continue
        call = re.compile(r"\b" + re.escape(name) + r"\s*\(")
        callers = [f for f, lines in plus_lines.items() if f != afile and any(call.search(l) for l in lines)]
        if callers:
            signature_changes.append({"func": name, "call_files": len(callers)})
    return signature_changes, moves


def candidates(repo, name, since):
    log = git(repo, "log", "--first-parent", f"--since={since}", "--format=%H%x00%P%x00%cs%x00%s%x00%b%x01", "HEAD")
    for rec in log.split("\x01"):
        rec = rec.strip("\n")
        if not rec:
            continue
        commit, parents, date, subject, body = rec.split("\x00")
        parent = parents.split()[0] if parents else None
        if not parent:
            continue
        title, pr = title_of(subject, body)
        if SKIP_TITLE.search(title):
            continue
        numstat = git(repo, "diff", "--numstat", parent, commit)
        code, tests, other = [], [], []
        added = deleted = 0
        for line in numstat.splitlines():
            a, d, path = line.split("\t", 2)
            if " => " in path:
                path = re.sub(r"\{[^}]* => ([^}]*)\}", r"\1", path).split(" => ")[-1]
            if path.endswith(".go") and not SKIP_PATH.search(path):
                if TEST_PATH.search(path):
                    tests.append(path)
                elif GENERATED.search(path):
                    other.append(path)
                else:
                    code.append(path)
                    added += int(a) if a != "-" else 0
                    deleted += int(d) if d != "-" else 0
            elif not SKIP_PATH.search(path):
                other.append(path)
        packages = sorted({os.path.dirname(p) for p in code})
        if len(packages) < 2 or not tests:
            continue
        sigs, moves = refactor_signals(repo, parent, commit, code)
        n = len(packages)
        yield {
            "repo": name,
            "pr": pr,
            "issues": sorted({int(n) for n in ISSUE.findall(body)} - {pr}),
            "title": title,
            "kind": "internal" if INTERNAL.search(title) else "feature",
            "commit": commit[:12],
            "parent": parent[:12],
            "date": date,
            "size": "S" if n == 2 else "M" if n <= 5 else "L",
            "packages": packages,
            "code_files": len(code),
            "lines_added": added,
            "lines_deleted": deleted,
            "test_files": sorted(tests),
            "other_files": len(other),
            "other_kinds": sorted({os.path.splitext(p)[1] or os.path.basename(p) for p in other}),
            "signature_changes": sigs,
            "moved_funcs": moves,
            "refactored": bool(sigs or moves),
        }


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("repo")
    ap.add_argument("name")
    ap.add_argument("--since", default="2026-07-01")
    args = ap.parse_args()
    for c in candidates(args.repo, args.name, args.since):
        json.dump(c, sys.stdout)
        sys.stdout.write("\n")


if __name__ == "__main__":
    main()
