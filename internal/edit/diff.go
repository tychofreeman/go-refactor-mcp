package edit

import (
	"bytes"
	"fmt"
	"sort"
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

// LineMap maps each line of before (1-based) to the line it is in after,
// or to 0 if the change rewrote or removed it.
func LineMap(before, after []byte) []int {
	a, b := splitLines(string(before)), splitLines(string(after))
	m := make([]int, len(a)+1)
	for _, o := range myers(a, b) {
		if o.kind == ' ' {
			m[o.a+1] = o.b + 1
		}
	}
	return m
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

// LineMapOf is LineMap for after, which edits made from before: a line no
// edit touched goes where the edits before it moved it, even where a line
// diff would pair other lines (code an edit moved below it, for one).
// Where after differs from what the edits make of before (gofmt), a line
// diff follows the lines from one to the other.
func LineMapOf(before, after []byte, edits []TextEdit) []int {
	return lineMapOf(before, after, edits, false)
}

// LineMapOfInPlace is LineMapOf for a change made the same at every site,
// such as a rename: a line an edit changes within, adding no line break,
// keeps its number.
func LineMapOfInPlace(before, after []byte, edits []TextEdit) []int {
	return lineMapOf(before, after, edits, true)
}

// lineMapOf is LineMapOf, and with inPlace LineMapOfInPlace.
func lineMapOf(before, after []byte, edits []TextEdit, inPlace bool) []int {
	applied, err := Apply(before, edits)
	if err != nil {
		return LineMap(before, after)
	}
	m := editLineMap(before, applied, edits, inPlace)
	if bytes.Equal(applied, after) {
		return m
	}
	// Formatting (gofmt) changed some lines: a line diff follows them, as
	// the changes are small and local. A line it only realigned is the
	// same line.
	formatted := LineMap(squeezeSpace(applied), squeezeSpace(after))
	for i, l := range m {
		m[i] = formatted[l]
	}
	return m
}

// PosMapOf maps byte offsets of before to after, which edits made from
// before (and gofmt may then have realigned): it returns the offset in
// after of what was at off in before, or -1 if an edit replaced it or
// formatting changed its line beyond realigning.
func PosMapOf(before, after []byte, edits []TextEdit) func(off int) int {
	applied, err := Apply(before, edits)
	if err != nil {
		applied, edits = before, nil
	}
	sorted := append([]TextEdit(nil), edits...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
	// throughEdits maps an offset of before to applied: an insertion at it
	// pushes it right, and a replaced range takes it away.
	throughEdits := func(off int) int {
		delta := 0
		for _, e := range sorted {
			switch {
			case e.Start == e.End && e.Start <= off, e.Start < e.End && e.End <= off:
				delta += len(e.NewText) - (e.End - e.Start)
			case e.Start <= off && off < e.End:
				return -1
			}
		}
		return off + delta
	}
	if bytes.Equal(applied, after) {
		return throughEdits
	}
	// Formatting changed some lines: follow a line whose spacing alone it
	// changed (gofmt moves no token), and in it the same byte, by the bytes
	// other than spaces before it.
	lines := LineMap(stripSpace(applied), stripSpace(after))
	return func(off int) int {
		off = throughEdits(off)
		if off < 0 || off > len(applied) {
			return -1
		}
		l := lines[bytes.Count(applied[:off], []byte("\n"))+1]
		if l == 0 {
			return -1
		}
		k := nonSpace(applied[bytes.LastIndexByte(applied[:off], '\n')+1 : off])
		j := 0
		for n := 1; n < l; n++ {
			j += bytes.IndexByte(after[j:], '\n') + 1
		}
		for n := 0; j < len(after); j++ {
			switch c := after[j]; {
			case c == '\n':
				if n == k {
					return j
				}
				return -1
			case c != ' ' && c != '\t':
				if n == k {
					return j
				}
				n++
			}
		}
		return -1
	}
}

// nonSpace counts the bytes of b other than spaces and tabs.
func nonSpace(b []byte) int {
	n := 0
	for _, c := range b {
		if c != ' ' && c != '\t' {
			n++
		}
	}
	return n
}

// squeezeSpace writes each line of src with its spaces and tabs made one
// space between words, so that lines gofmt only realigned compare equal.
func squeezeSpace(src []byte) []byte {
	var b bytes.Buffer
	for _, l := range splitLines(string(src)) {
		b.WriteString(strings.Join(strings.Fields(l), " "))
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// stripSpace writes each line of src without its spaces and tabs, so that
// lines differing only in spacing compare equal.
func stripSpace(src []byte) []byte {
	var b bytes.Buffer
	for _, l := range splitLines(string(src)) {
		b.WriteString(strings.Join(strings.Fields(l), ""))
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// editLineMap is LineMap for applied, which is exactly what edits make of
// before.
func editLineMap(before, applied []byte, edits []TextEdit, inPlace bool) []int {
	starts := map[int]int{0: 1} // applied's line numbers, by the offset each starts at
	for i, c := range applied {
		if c == '\n' {
			starts[i+1] = len(starts) + 1
		}
	}
	lines := splitLines(string(before))
	m := make([]int, len(lines)+1)
	s := 0 // where line i starts in before
	for i, l := range lines {
		text := s + len(strings.TrimSuffix(l, "\n")) // where its newline is
		at, touched, within := s, false, false       // where it starts in applied
		for _, e := range edits {
			switch {
			case e.End <= s:
				at += len(e.NewText) - (e.End - e.Start)
			case e.Start < text && inPlace && e.Start >= s && e.End <= text && !strings.Contains(e.NewText, "\n"):
				within = true
			case e.Start < text:
				touched = true
			}
		}
		switch {
		case touched:
		case within:
			m[i+1] = starts[at] // changed in place: the same line
		// An edit at the newline, or one just before the line, may still have
		// changed it ("x" glued to its start): its text must be there, as a line.
		case at+len(l) <= len(applied) && string(applied[at:at+len(l)]) == l:
			m[i+1] = starts[at]
		}
		s += len(l)
	}
	return m
}
