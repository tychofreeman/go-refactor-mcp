#!/bin/sh
# test-times.sh FILE TITLE reports the slowest packages, tests and subtests
# in FILE, the output of go test -json: in the log, in the job summary, and
# as a notice annotation, which the GitHub API serves without the logs.
#
# Elapsed times overlap: packages run in parallel, and so do the golden
# cases, which are subtests. The sum of package times is CPU-like work, not
# wall time.
set -eu
json=$1
title=$2

report=$(jq -cR 'fromjson? | select((.Action == "pass" or .Action == "fail") and .Elapsed != null)' "$json" | jq -rs '
  def short: sub("^github.com/tychofreeman/go-refactor-mcp/"; "");
  def rows(f; n):
    map(select(f)) | sort_by(-.Elapsed) | .[:n][]
    | "\(.Elapsed)s  \(.Package | short)\(if .Test then "  " + .Test else "" end)";
  "Sum of package times: \(map(select(.Test == null) | .Elapsed) | add // 0 | . * 10 | round / 10)s",
  "",
  "Slowest packages:", rows(.Test == null; 12),
  "",
  "Slowest tests:", rows(.Test != null and (.Test | contains("/") | not); 12),
  "",
  "Slowest subtests:", rows(.Test != null and (.Test | contains("/")); 8)')

printf '%s\n' "$report"
printf '### Test times, %s\n\n```\n%s\n```\n' "$title" "$report" >> "${GITHUB_STEP_SUMMARY:-/dev/null}"
encoded=$(printf '%s' "$report" | sed -e 's/%/%25/g' -e 's/\r/%0D/g' | awk 'BEGIN { ORS = "%0A" } 1')
echo "::notice title=Test times on ${title}::${encoded}"
