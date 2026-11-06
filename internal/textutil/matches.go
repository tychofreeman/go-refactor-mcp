package textutil

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// MaxMatches is the largest number of lines a pattern lists.
const MaxMatches = 100

// Matches lists the lines of each file of paths that match re, grouped by
// file and numbered, at most MaxMatches in all; rel names each file and
// expr is how re was written.
func Matches(paths []string, re *regexp.Regexp, rel func(path string) string, expr string) (string, error) {
	var sb strings.Builder
	shown, total := 0, 0
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		lines := strings.SplitAfter(string(src), "\n")
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		var hits []int
		for i, line := range lines {
			if re.MatchString(strings.TrimRight(line, "\n")) {
				hits = append(hits, i+1)
			}
		}
		if len(hits) == 0 {
			continue
		}
		total += len(hits)
		if shown == MaxMatches {
			continue
		}
		verb := "lines match"
		if len(hits) == 1 {
			verb = "line matches"
		}
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "%s  %d %s /%s/:\n", rel(path), len(hits), verb, expr)
		hits = hits[:min(len(hits), MaxMatches-shown)]
		shown += len(hits)
		width := len(fmt.Sprint(hits[len(hits)-1]))
		for _, l := range hits {
			sb.WriteString(NumberedLine(width, l, lines[l-1]))
		}
	}
	if total == 0 {
		return fmt.Sprintf("No line matches /%s/.\n", expr), nil
	}
	if shown < total {
		fmt.Fprintf(&sb, "\nShown %d of %d matching lines; narrow the pattern, or pass file.\n", shown, total)
	}
	return sb.String(), nil
}
