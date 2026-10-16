package edit

import (
	"fmt"
	"strings"
)

// Unified returns a unified diff (3 lines of context) between before and
// after, labelled with name. It returns "" when the inputs are equal.
//
// The diff is what the model sees after each refactoring, so it is kept
// close to `diff -u` / `git diff` output, which models read well.
func Unified(name string, before, after []byte) string {
	if string(before) == string(after) {
		return ""
	}
	a, b := splitLines(string(before)), splitLines(string(after))
	ops := myers(a, b)
	hunks := group(ops, 3)

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
