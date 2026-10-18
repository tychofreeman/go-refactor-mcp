package edit

import (
	"fmt"
	"strconv"
	"strings"
)

// Unified returns a unified diff (3 lines of context) between before and
// after, labelled with name. It returns "" when the inputs are equal.
//
// The diff is what the model sees after each refactoring, so it is kept
// close to `diff -u` / `git diff` output, which models read well.
func Unified(name string, before, after []byte) string {
	return UnifiedContext(name, before, after, 3)
}

// UnifiedContext is Unified with ctx lines of context; with none, the
// diff is just the changed lines and where they are.
func UnifiedContext(name string, before, after []byte, ctx int) string {
	if string(before) == string(after) {
		return ""
	}
	a, b := splitLines(string(before)), splitLines(string(after))
	ops := myers(a, b)
	hunks := group(ops, ctx)

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- a/%s\n+++ b/%s\n", name, name)
	for _, h := range hunks {
		fmt.Fprintf(&sb, "@@ -%s +%s @@\n", rng(h.aStart, h.aLen), rng(h.bStart, h.bLen))
		for _, o := range h.ops {
			var line string
			switch o.kind {
			case ' ', '-':
				line = a[o.a]
			case '+':
				line = b[o.b]
			}
			sb.WriteByte(o.kind)
			sb.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				sb.WriteString("\n\\ No newline at end of file\n")
			}
		}
	}
	return sb.String()
}

// Locations lists where a unified diff changed each file, by line in the
// new file ("changed lines:\n  a.go: 12, 40-41\n"): enough to report a
// change that is the same everywhere, such as a rename.
func Locations(diff string) string {
	var sb strings.Builder
	sb.WriteString("changed lines:\n")
	file, lines := "", []string(nil)
	flush := func() {
		if file != "" {
			fmt.Fprintf(&sb, "  %s: %s\n", file, strings.Join(lines, ", "))
		}
		file, lines = "", nil
	}
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+++ b/"):
			flush()
			file = strings.TrimPrefix(l, "+++ b/")
		case strings.HasPrefix(l, "@@ "):
			start, n := newRange(l)
			switch n {
			case 0:
				lines = append(lines, fmt.Sprintf("after %d", start))
			case 1:
				lines = append(lines, strconv.Itoa(start))
			default:
				lines = append(lines, fmt.Sprintf("%d-%d", start, start+n-1))
			}
		}
	}
	flush()
	return sb.String()
}

// ElideSupplied shortens a unified diff of a change whose new code the
// model wrote itself: each run of two or more added lines that repeat
// supplied (ignoring whitespace) is replaced by one line saying how many
// lines were added and where they are now. Added lines the model did not
// write, such as imports, are kept.
func ElideSupplied(diff, supplied string) string {
	if supplied == "" {
		return diff
	}
	written := map[string]bool{"": true}
	for _, l := range strings.Split(supplied, "\n") {
		written[squash(l)] = true
	}
	var sb strings.Builder
	var run []string
	line, start := 0, 0 // the next added or context line's number in the new file; the run's first
	flush := func() {
		if len(run) >= 2 {
			fmt.Fprintf(&sb, "[%d lines added as written: lines %d-%d]\n", len(run), start, start+len(run)-1)
		} else {
			sb.WriteString(strings.Join(run, ""))
		}
		run = nil
	}
	for _, l := range strings.SplitAfter(diff, "\n") {
		header := strings.HasPrefix(l, "--- a/") || strings.HasPrefix(l, "+++ b/")
		if !header && strings.HasPrefix(l, "+") && written[squash(l[1:])] {
			if run == nil {
				start = line
			}
			run = append(run, l)
			line++
			continue
		}
		flush()
		switch {
		case header:
		case strings.HasPrefix(l, "@@ "):
			line = newStart(l)
		case strings.HasPrefix(l, "+"), strings.HasPrefix(l, " "):
			line++
		}
		sb.WriteString(l)
	}
	flush()
	return sb.String()
}

// newStart is the first line in the new file of the hunk whose header is
// h ("@@ -a,b +c,d @@").
func newStart(h string) int {
	start, _ := newRange(h)
	return start
}

// newRange is the new-file range of the hunk whose header is h
// ("@@ -a,b +c,d @@"): its first line and how many lines it has.
func newRange(h string) (start, n int) {
	_, after, _ := strings.Cut(h, " +")
	rng, _, _ := strings.Cut(after, " ")
	s, count, found := strings.Cut(rng, ",")
	start, _ = strconv.Atoi(s)
	n = 1
	if found {
		n, _ = strconv.Atoi(count)
	}
	return start, n
}

// squash drops the whitespace from a line, so a line gofmt re-spaced still
// matches the one the model wrote.
func squash(s string) string { return strings.Join(strings.Fields(s), "") }

func rng(start, n int) string {
	// Unified format: 1-based start; an empty range points at the line before.
	if n == 0 {
		return fmt.Sprintf("%d,0", start)
	}
	if n == 1 {
		return fmt.Sprintf("%d", start+1)
	}
	return fmt.Sprintf("%d,%d", start+1, n)
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// op is one line of an edit script: ' ' keep a[a]==b[b], '-' delete a[a],
// '+' insert b[b].
type op struct {
	kind byte
	a, b int
}

// myers computes a shortest edit script using Myers' O(ND) algorithm.
// Refactoring diffs are small (D is small), so storing one V snapshot per D
// is cheap.
func myers(a, b []string) []op {
	n, m := len(a), len(b)
	max := n + m
	if max == 0 {
		return nil
	}
	off := max + 1
	v := make([]int, 2*max+3)
	var trace [][]int

	var done bool
	for d := 0; d <= max && !done; d++ {
		trace = append(trace, append([]int(nil), v...))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1] // move down (insertion)
			} else {
				x = v[off+k-1] + 1 // move right (deletion)
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				done = true
				break
			}
		}
	}

	// Backtrack from (n, m) to (0, 0).
	var rev []op
	x, y := n, m
	for d := len(trace) - 1; d >= 0; d-- {
		vd := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && vd[off+k-1] < vd[off+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := vd[off+prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			x--
			y--
			rev = append(rev, op{' ', x, y})
		}
		if d > 0 {
			if x == prevX {
				y--
				rev = append(rev, op{'+', x, y})
			} else {
				x--
				rev = append(rev, op{'-', x, y})
			}
		}
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

type hunk struct {
	aStart, aLen, bStart, bLen int
	ops                        []op
}

// group splits an edit script into hunks with ctx lines of context.
func group(ops []op, ctx int) []hunk {
	var hunks []hunk
	i := 0
	for i < len(ops) {
		// Find the next change.
		for i < len(ops) && ops[i].kind == ' ' {
			i++
		}
		if i == len(ops) {
			break
		}
		start := i - ctx
		if start < 0 {
			start = 0
		}
		// Extend while changes are within 2*ctx of each other.
		end := i
		for end < len(ops) {
			if ops[end].kind != ' ' {
				end++
				continue
			}
			run := end
			for run < len(ops) && ops[run].kind == ' ' {
				run++
			}
			if run == len(ops) || run-end > 2*ctx {
				end += min(ctx, run-end)
				break
			}
			end = run
		}
		h := hunk{ops: ops[start:end]}
		first := ops[start]
		h.aStart, h.bStart = first.a, first.b
		for _, o := range h.ops {
			if o.kind != '+' {
				h.aLen++
			}
			if o.kind != '-' {
				h.bLen++
			}
		}
		hunks = append(hunks, h)
		i = end
	}
	return hunks
}
