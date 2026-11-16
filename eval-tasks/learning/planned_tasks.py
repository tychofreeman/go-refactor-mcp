"""Writes a task file whose prompts begin with the loop's focused view of
the module (probe.focused: what the prompt names, the functions that call
them, use counts from the type checker, every package's names), for
cmd/refactor-ab's planned arm: a session that plans from the view as the
loop's plan does. Task names stay the same, so results set beside the
mcp arm's.

Run from the repository root:
python3 eval-tasks/learning/planned_tasks.py eval-tasks/derivations-freight-xxl.json eval-tasks/derivations-freight-xxl-planned.json
"""
import json
import os
import subprocess
import sys
import tempfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import probe  # noqa: E402

PROMPT = """A view of the module as far as this change reaches, from the type
checker, with paths relative to the module's root:

{view}
The change:
{prompt}"""


def main():
    src, dst = sys.argv[1], sys.argv[2]
    with tempfile.TemporaryDirectory() as tmp:
        binary = os.path.join(tmp, "refactor")
        subprocess.run(["go", "build", "-o", binary, "./cmd/refactor"], check=True)
        tasks = json.load(open(src))
        base = os.path.dirname(src)
        for t in tasks:
            module = os.path.join(base, t.get("dir", "."))
            t["prompt"] = PROMPT.format(view=probe.focused(binary, module, t["prompt"]), prompt=t["prompt"])
    with open(dst, "w") as f:
        json.dump(tasks, f, indent=2)
        f.write("\n")


if __name__ == "__main__":
    main()
