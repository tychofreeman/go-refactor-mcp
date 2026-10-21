"""Print the "## Lessons" section of each run's final message in an A/B
run directory, with whether the run passed and what it cost.

    python3 eval-tasks/learning/lessons.py eval-runs/NAME
"""
import json
import pathlib
import sys


def final_message(transcript):
    for line in transcript.open():
        event = json.loads(line)
        if event.get("type") == "result":
            return event.get("result") or ""
    return ""


def lessons(text):
    i = text.find("## Lessons")
    return text[i:].strip() if i >= 0 else "(no lessons section)"


def main(run_dir):
    run_dir = pathlib.Path(run_dir)
    for line in (run_dir / "results.jsonl").open():
        r = json.loads(line)
        transcript = run_dir / f"{r['task']}-{r['arm']}-{r['run']}.jsonl"
        status = "passed" if r["passed"] else "failed"
        print(f"### {r['task']} run {r['run']}: {status}, ${r['cost_usd']:.3f}\n")
        print(lessons(final_message(transcript)) if transcript.exists() else "(no transcript)")
        print()


if __name__ == "__main__":
    main(sys.argv[1])
