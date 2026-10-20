#!/usr/bin/env python3
"""Check whether a candidate's own tests can be an A/B task's hidden check.

    verify.py REPO_DIR CANDIDATES.jsonl [--only N,N] [--timeout 600] > OUT.jsonl

For each candidate (from candidates.py) of the repository at REPO_DIR:
the PR's test files are put onto its parent commit, and the tests the PR
added or changed are compiled and run there, then run again at the PR's
commit. The outcome is one of:

  black_box   some package's tests compile against the old code, fail
              there, and pass after the PR: those packages (listed) can be
              the check, and a prompt can describe the feature freely.
  needs_api   no such package, but some tests use names the PR added
              (listed in "missing"): the prompt must name them, or they
              rule out other designs.
  not_f2p     the tests already pass before the PR: they do not check it.
  broken      the tests fail after the PR too, or none could be found
              (flaky, or they need something this machine lacks).

Each package is judged on its own and listed under the outcome it got.

The repository is left checked out at its original commit. Go comes from
PATH; lazygit's integration tests run through pkg/integration/clients.
"""

import argparse
import json
import os
import re
import subprocess
import sys
import time

LAZYGIT_TESTS = "pkg/integration/tests/"
MISSING = [
    re.compile(r"undefined: ([\w.]+)"),
    re.compile(r"has no field or method (\w+)"),
    re.compile(r"unknown field (\w+) in struct literal of type ([\w.]+)"),
    re.compile(r"(not enough|too many) arguments in call to ([\w.]+)"),
    re.compile(r"cannot use .* as ([\w.\[\]*]+) value in"),
]


def run(repo, *cmd, timeout=600, env=None):
    t0 = time.time()
    try:
        p = subprocess.run(cmd, cwd=repo, capture_output=True, text=True, timeout=timeout, env=env)
        return p.returncode, p.stdout + p.stderr, round(time.time() - t0, 1)
    except subprocess.TimeoutExpired as e:
        out = (e.stdout or b"").decode(errors="replace") if isinstance(e.stdout, bytes) else (e.stdout or "")
        return "timeout", out, round(time.time() - t0, 1)


def git(repo, *args):
    return subprocess.run(["git", "-C", repo, *args], check=True, capture_output=True, text=True).stdout


def checkout(repo, rev):
    git(repo, "checkout", "-qf", "--detach", rev)
    git(repo, "clean", "-fdq")


def put_tests(repo, commit, tests):
    """Puts the commit's version of each test file in the work tree."""
    present = set(git(repo, "ls-tree", "-r", "--name-only", commit, "--", *tests).split())
    for path in tests:
        if path in present:
            git(repo, "checkout", commit, "--", path)
        elif os.path.exists(os.path.join(repo, path)):
            os.remove(os.path.join(repo, path))


def changed_tests(repo, parent, commit, tests):
    """The top-level test functions the PR added or changed, by package
    directory, and lazygit integration tests by name."""
    go_tests, integration = {}, set()
    diff = git(repo, "diff", "-U0", parent, commit, "--", *tests)
    path = None
    for line in diff.splitlines():
        if line.startswith("+++ "):
            path = line[6:] if line.startswith("+++ b/") else None
            continue
        if not path:
            continue
        if path.startswith(LAZYGIT_TESTS):
            name = path[len(LAZYGIT_TESTS):-3]
            if "/" in name and not name.startswith("shared/"):
                integration.add(name)
            continue
        if not path.endswith("_test.go"):
            continue
        names = []
        if line.startswith("@@"):
            names = re.findall(r"func (Test\w+|Benchmark\w+|Example\w*)\(", line)
        elif line.startswith("+"):
            names = re.findall(r"^\+func (Test\w+|Example\w*)\(", line)
        for n in names:
            if not n.startswith("Benchmark"):
                go_tests.setdefault(os.path.dirname(path) or ".", set()).add(n)
    return {d: sorted(n) for d, n in go_tests.items()}, sorted(integration)


def missing_names(output):
    found = []
    for line in output.splitlines():
        for rx in MISSING:
            m = rx.search(line)
            if m:
                found.append(" ".join(g for g in m.groups() if g))
    return sorted(set(found))


def go_test(repo, go_tests, timeout, compile_only=False):
    pkgs = ["./" + d for d in sorted(go_tests)]
    names = sorted({n for ns in go_tests.values() for n in ns})
    pattern = "^$" if compile_only else "^(" + "|".join(names) + ")$"
    return run(repo, "go", "test", "-count=1", "-vet=off", "-run", pattern, *pkgs, timeout=timeout)


def lazygit_run(repo, names, timeout):
    """Runs lazygit integration tests through its go test runner, which
    drives lazygit headless in a pseudo-terminal (its cli runner needs a
    real terminal)."""
    code, out, total = 0, "", 0.0
    for name in names:
        pattern = "/".join(["^TestIntegration$"] + ["^" + re.escape(p) + "$" for p in name.split("/")])
        c, o, secs = run(repo, "go", "test", "-count=1", "-v", "-vet=off", "-run", pattern, "./pkg/integration/clients/", timeout=timeout)
        if c == 0 and "--- PASS: TestIntegration/" not in o:
            c, o = 1, o + "\nno test named " + name + " ran"
        code = code or c
        out += o
        total += secs
    return code, out, round(total, 1)


def lazygit_compile(repo, timeout):
    return run(repo, "go", "build", "-o", os.devnull, "./cmd/integration_test", "./pkg/integration/...", timeout=timeout)


def verify(repo, c, timeout):
    parent, commit, tests = c["parent"], c["commit"], c["test_files"]
    go_tests, integration = changed_tests(repo, parent, commit, tests)
    rec = {"go_tests": go_tests, "integration_tests": integration}
    if not go_tests and not integration:
        return dict(rec, outcome="broken", why="no added or changed test functions")

    checkout(repo, parent)
    put_tests(repo, commit, tests)
    steps = []
    # Each package's tests are judged on their own: one package's tests may
    # use a helper the PR added while another's check behavior only.
    black_box, needs_api, not_f2p = [], [], []
    missing, errors = [], []
    for d in sorted(go_tests):
        one = {d: go_tests[d]}
        code, out, secs = go_test(repo, one, timeout, compile_only=True)
        steps.append(("compile_before " + d, code, secs))
        if code != 0:
            needs_api.append(d)
            missing += missing_names(out)
            errors += [l for l in out.splitlines() if ".go:" in l][:4]
            continue
        code, out, secs = go_test(repo, one, timeout)
        steps.append(("run_before " + d, code, secs))
        (black_box if code != 0 else not_f2p).append(d)
    if integration:
        code, out, secs = lazygit_compile(repo, timeout)
        steps.append(("compile_before integration", code, secs))
        if code != 0:
            needs_api.append("integration")
            missing += missing_names(out)
            errors += [l for l in out.splitlines() if ".go:" in l][:4]
        else:
            code, out, secs = lazygit_run(repo, integration, timeout)
            steps.append(("run_before integration", code, secs))
            (black_box if code != 0 else not_f2p).append("integration")

    checkout(repo, commit)
    failing_after = []
    after_out = ""
    for d in sorted(go_tests):
        code, out, secs = go_test(repo, {d: go_tests[d]}, timeout)
        steps.append(("run_after " + d, code, secs))
        if code != 0:
            failing_after.append(d)
            after_out += out
    if integration:
        code, out, secs = lazygit_run(repo, integration, timeout)
        steps.append(("run_after integration", code, secs))
        if code != 0:
            failing_after.append("integration")
            after_out += out
    rec["steps"] = [{"step": s, "exit": e, "seconds": t} for s, e, t in steps]
    rec.update(
        black_box=[p for p in black_box if p not in failing_after],
        needs_api=[p for p in needs_api if p not in failing_after],
        not_f2p=not_f2p,
        failing_after=failing_after,
    )
    if missing:
        rec["missing"] = sorted(set(missing))
        rec["compile_errors"] = errors[:8]
    if failing_after:
        rec["output_tail"] = [l for l in after_out.splitlines() if l.strip()][-6:]
    if rec["black_box"]:
        return dict(rec, outcome="black_box")
    if rec["needs_api"]:
        return dict(rec, outcome="needs_api")
    if failing_after:
        return dict(rec, outcome="broken", why="tests fail at the PR's commit")
    return dict(rec, outcome="not_f2p")


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("repo")
    ap.add_argument("candidates")
    ap.add_argument("--only", default="", help="commits to verify, comma-separated (default: every feature)")
    ap.add_argument("--timeout", type=int, default=600, help="seconds per go command")
    ap.add_argument("--clean-cache", action="store_true", help="empty Go's build cache after each candidate (each builds its own test binaries; a large repository fills a disk in a few dozen)")
    args = ap.parse_args()
    only = {s for s in args.only.split(",") if s}
    start = git(args.repo, "rev-parse", "HEAD").strip()
    try:
        with open(args.candidates) as f:
            for line in f:
                c = json.loads(line)
                if c["kind"] != "feature" or (only and c["commit"] not in only):
                    continue
                t0 = time.time()
                try:
                    v = verify(args.repo, c, args.timeout)
                except subprocess.CalledProcessError as e:
                    v = {"outcome": "broken", "why": f"git: {e.stderr.strip()[:200]}"}
                v.update(repo=c["repo"], commit=c["commit"], seconds=round(time.time() - t0, 1))
                print(json.dumps(v), flush=True)
                if args.clean_cache:
                    run(args.repo, "go", "clean", "-cache")
    finally:
        checkout(args.repo, start)


if __name__ == "__main__":
    main()
