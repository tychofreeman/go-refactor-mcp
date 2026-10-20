#!/usr/bin/env python3
"""Builds the third-party A/B tasks (ab.json) from tasks.json.

    tasks.py clone [--from DIR]   clones the repositories into clones/
    tasks.py build                writes ab.json, and tests/NAME/ from the clones
    tasks.py validate [NAME ...]  checks each task as cmd/refactor-ab runs it

tasks.json holds what is written by hand: each task's name, repository,
the PR's commit and the prompt. The rest comes from candidates.jsonl: the
task starts at the PR's parent, its hidden tests are the PR's versions of
the black-box test files and fixtures, and its check runs them. Clones
and extracted tests stay out of git (the tests belong to their projects).

validate makes a copy as the harness does (git archive of a revision, the
hidden tests copied over) and runs the check twice: at the parent it must
fail, at the PR's commit it must pass.
"""
import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time

HERE = os.path.dirname(os.path.abspath(__file__))
CLONES = os.path.join(HERE, "clones")
TESTS = os.path.join(HERE, "tests")

# Before the PR's tests, everything must still build. Not `go vet ./...`:
# on Hugo, cold, it took 20 minutes and 18 GB of build space. So a run that
# breaks a test in another package is not caught.
CHECK = "go build ./... && "

# The hidden tests are the PR's versions of whole test files, and some are
# internal tests that use the package's functions, types and fields as they
# were. Every prompt says so, or a run that refactored them would fail
# however well it delivered the feature.
HIDDEN = ("Tests you cannot see will check this. They use the existing functions, types and fields "
          "by their current names and signatures, so keep those as they are; refactor around them as you need.")


def clone_dir(repo):
    return os.path.join(CLONES, repo.replace("/", "_"))


def load():
    with open(os.path.join(HERE, "tasks.json")) as f:
        tasks = json.load(f)
    with open(os.path.join(HERE, "candidates.jsonl")) as f:
        cands = [json.loads(line) for line in f]
    out = []
    for t in tasks:
        c = [c for c in cands if c["repo"] == t["repo"] and c["commit"].startswith(t["commit"])]
        if len(c) != 1:
            sys.exit(f"{t['name']}: {len(c)} candidates match {t['repo']} {t['commit']}")
        if not c[0].get("hidden_check"):
            sys.exit(f"{t['name']}: candidate has no hidden check")
        out.append((t, c[0]))
    return out


def ab_task(t, c):
    return {
        "name": t["name"],
        "dir": os.path.relpath(clone_dir(t["repo"]), HERE),
        "rev": c["parent"],
        "prompt": t["prompt"] + "\n\n" + HIDDEN,
        "tests": os.path.relpath(os.path.join(TESTS, t["name"]), HERE),
        "check": CHECK + c["hidden_check"]["command"],
    }


def git(repo, *args, **kw):
    return subprocess.run(["git", "-C", repo, *args], check=True, capture_output=True, text=True, **kw).stdout


def extract(repo, rev, files, dest):
    """Writes rev's version of files under dest; files rev lacks are left out."""
    present = git(repo, "ls-tree", "-r", "--name-only", rev, "--", *files).split()
    if present:
        archive = subprocess.run(["git", "-C", repo, "archive", rev, "--", *present], check=True, capture_output=True).stdout
        subprocess.run(["tar", "-x", "-C", dest], input=archive, check=True)


def cmd_clone(args):
    os.makedirs(CLONES, exist_ok=True)
    for repo in sorted({t["repo"] for t, _ in load()}):
        d = clone_dir(repo)
        if os.path.exists(d):
            print(f"{repo}: {d} exists")
            continue
        if args.source:
            os.symlink(os.path.join(os.path.abspath(args.source), os.path.basename(d)), d)
            print(f"{repo}: linked")
            continue
        # Blobless: the history is needed for the parents, file contents
        # only for the revisions archived.
        subprocess.run(["git", "clone", "-q", "--filter=blob:none", f"https://github.com/{repo}.git", d], check=True)
        print(f"{repo}: cloned")


def cmd_build(args):
    tasks = load()
    names = [t["name"] for t, _ in tasks]
    if len(set(names)) != len(names):
        sys.exit("task names are not unique")
    for t, c in tasks:
        dest = os.path.join(TESTS, t["name"])
        shutil.rmtree(dest, ignore_errors=True)
        os.makedirs(dest)
        extract(clone_dir(t["repo"]), c["commit"], c["hidden_check"]["files"], dest)
    with open(os.path.join(HERE, "ab.json"), "w") as f:
        json.dump([ab_task(t, c) for t, c in tasks], f, indent=2)
        f.write("\n")
    print(f"{len(tasks)} tasks")


def run_checks(repo, rev, tests, checks, timeout):
    """Runs each check in turn in one copy of rev with the tests over it."""
    results = []
    with tempfile.TemporaryDirectory() as dir:
        archive = subprocess.run(["git", "-C", repo, "archive", rev], check=True, capture_output=True).stdout
        subprocess.run(["tar", "-x", "-C", dir], input=archive, check=True)
        shutil.copytree(tests, dir, dirs_exist_ok=True)
        for check in checks:
            t0 = time.time()
            try:
                p = subprocess.run(["sh", "-c", check], cwd=dir, capture_output=True, text=True, errors="replace", timeout=timeout)
                code, out = p.returncode, p.stdout + p.stderr
            except subprocess.TimeoutExpired:
                code, out = "timeout", ""
            results.append({"exit": code, "seconds": round(time.time() - t0, 1), "tail": out.strip().splitlines()[-8:]})
    return results


def cmd_validate(args):
    ok = True
    for t, c in load():
        if args.names and t["name"] not in args.names:
            continue
        a = ab_task(t, c)
        repo, tests = os.path.join(HERE, a["dir"]), os.path.join(HERE, a["tests"])
        hidden = c["hidden_check"]["command"]
        # At the parent the code must build and the hidden tests must fail;
        # at the PR's commit the whole check must pass.
        build, before = run_checks(repo, c["parent"], tests, [CHECK.removesuffix(" && "), hidden], args.timeout)
        after, = run_checks(repo, c["commit"], tests, [a["check"]], args.timeout)
        good = build["exit"] == 0 and before["exit"] not in (0, "timeout") and after["exit"] == 0
        ok = ok and good
        if after["exit"] == 0:
            after["tail"] = []
        print(json.dumps({"name": t["name"], "valid": good, "build_before": build, "before": before, "after": after}), flush=True)
        if args.clean_cache:
            subprocess.run(["go", "clean", "-cache"], check=False)
    sys.exit(0 if ok else 1)


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    c = sub.add_parser("clone")
    c.add_argument("--from", dest="source", help="link existing clones in DIR (named OWNER_REPO) instead of cloning")
    sub.add_parser("build")
    v = sub.add_parser("validate")
    v.add_argument("names", nargs="*")
    v.add_argument("--timeout", type=int, default=1800, help="seconds per check")
    v.add_argument("--clean-cache", action="store_true", help="empty Go's build cache after each task (a cold Hugo or Gitea check takes over 15 GB)")
    args = ap.parse_args()
    {"clone": cmd_clone, "build": cmd_build, "validate": cmd_validate}[args.cmd](args)


if __name__ == "__main__":
    main()
