package refactor

// References to what earlier steps of a batch made.
//
// A step may take an id, and a later step's line, start_line or end_line
// may be a reference to an earlier step's lines in the file it names:
// "$helper.end", "$2.start" (by position), "$prev.end" (the step before),
// with an offset such as "$prev.end+1". A step's lines in a file are the
// ones it added or changed, from the first to the last, as they are now:
// they follow the later steps. RunBatch resolves references before the
// step's tool sees its arguments.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
)

// stepLines is what one step of a batch made in one file.
type stepLines struct {
	// lines are its new lines, as they are now; 0 once a later step
	// rewrote one.
	lines []int
	// point is, for a step that only removed lines, the line after them,
	// as it is now.
	point int
	// places is how many separate places of the file the step changed,
	// leaving out its imports.
	places int
	// rewrittenBy is the later step that rewrote the last of its lines.
	rewrittenBy int
}

// madeLines returns what a step made in a file it changed from before to
// after with edits (its plan's, against before), which moved before's
// lines as moved says (see edit.LineMap). A file it created is all
// new. Lines in the import declarations are left out: a step that adds an
// import still made one place, the code that needs it.
func madeLines(before, after []byte, moved []int, edits []edit.TextEdit, created bool) *stepLines {
	n := bytes.Count(after, []byte("\n"))
	if len(after) > 0 && after[len(after)-1] != '\n' {
		n++
	}
	if created {
		s := &stepLines{places: 1}
		for l := 1; l <= n; l++ {
			s.lines = append(s.lines, l)
		}
		return s
	}
	s := &stepLines{places: places(before, edits)}
	kept := map[int]bool{}
	for _, l := range moved {
		kept[l] = true
	}
	first, last := importLines(after)
	for l := 1; l <= n; l++ {
		if !kept[l] && (l < first || l > last) {
			s.lines = append(s.lines, l)
		}
	}
	if len(s.lines) == 0 {
		// Only removed: the point is where the next line kept went.
		s.point = n + 1
		for l := 1; l < len(moved); l++ {
			if moved[l] == 0 {
				for k := l + 1; k < len(moved); k++ {
					if moved[k] != 0 {
						s.point = moved[k]
						break
					}
				}
				break
			}
		}
	}
	return s
}

// places counts the separate places edits change in src: edits on the
// same or adjacent lines are one place, and edits in the import
// declarations none.
func places(src []byte, edits []edit.TextEdit) int {
	sorted := append([]edit.TextEdit(nil), edits...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
	line := func(off int) int { return bytes.Count(src[:min(off, len(src))], []byte("\n")) + 1 }
	first, last := importLines(src)
	count, end := 0, -1
	for _, e := range sorted {
		start, stop := line(e.Start), line(e.End)
		if start >= first && stop <= last || strings.HasPrefix(strings.TrimSpace(e.NewText), "import") {
			continue
		}
		if end < 0 || start > end+1 {
			count++
		}
		end = max(end, stop)
	}
	return max(count, 1)
}

// importLines returns the first and last line of src's import
// declarations, or 0 and -1 if it has none (or does not parse).
func importLines(src []byte) (first, last int) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ImportsOnly)
	if err != nil || len(f.Decls) == 0 {
		return 0, -1
	}
	return fset.Position(f.Decls[0].Pos()).Line, fset.Position(f.Decls[len(f.Decls)-1].End()).Line
}

// follow moves s's lines through a later step, which moved the file's
// lines as moved says (see edit.LineMap) and left it as after.
func (s *stepLines) follow(step int, moved []int, after []byte) {
	at := func(l int) int {
		if l < len(moved) {
			return moved[l]
		}
		// Past the end of before: the end of after.
		return l - len(moved) + len(edit.LineMap(after, after))
	}
	live := false
	for i, l := range s.lines {
		if l != 0 {
			s.lines[i] = at(l)
			live = live || s.lines[i] != 0
		}
	}
	if s.point != 0 {
		s.point = at(s.point)
		live = s.point != 0
	}
	if !live && s.rewrittenBy == 0 {
		s.rewrittenBy = step
	}
}

// span returns the first and last of s's lines as they are now.
func (s *stepLines) span() (first, last int) {
	if s.point != 0 {
		return s.point, s.point
	}
	for _, l := range s.lines {
		if l == 0 {
			continue
		}
		if first == 0 || l < first {
			first = l
		}
		last = max(last, l)
	}
	return first, last
}

var (
	stepRef = regexp.MustCompile(`^\$(prev|[0-9]+|[A-Za-z_][A-Za-z0-9_]*)\.(start|end)([+-][0-9]+)?$`)
	stepID  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// checkIDs refuses ids that are not names, are prev, or repeat.
func checkIDs(steps []Step) error {
	seen := map[string]int{}
	for i, s := range steps {
		switch {
		case s.ID == "":
			continue
		case !stepID.MatchString(s.ID) || s.ID == "prev":
			return fmt.Errorf("step %d has id %q; an id is a name (letters, digits and _, not starting with a digit), and not prev", i+1, s.ID)
		case seen[s.ID] != 0:
			return fmt.Errorf("steps %d and %d both have id %q; give each step its own", seen[s.ID], i+1, s.ID)
		}
		seen[s.ID] = i + 1
	}
	return nil
}

// resolveRefs replaces references in step i's line arguments with the
// lines they name in file rel, and returns the keys it resolved, which
// are line numbers as they are now.
func resolveRefs(args json.RawMessage, rel string, i int, steps []Step, made []map[string]*stepLines) (json.RawMessage, map[string]bool, error) {
	var a map[string]json.RawMessage
	if err := json.Unmarshal(args, &a); err != nil {
		return args, nil, nil
	}
	resolved := map[string]bool{}
	for _, key := range []string{"line", "start_line", "end_line"} {
		var ref string
		if raw, ok := a[key]; !ok || json.Unmarshal(raw, &ref) != nil {
			continue
		}
		n, err := resolveRef(ref, key, rel, i, steps, made)
		if err != nil {
			return nil, nil, err
		}
		a[key], _ = json.Marshal(n)
		resolved[key] = true
	}
	if len(resolved) == 0 {
		return args, nil, nil
	}
	out, err := json.Marshal(a)
	return out, resolved, err
}

// resolveRef returns the line ref names in file rel, for step i.
func resolveRef(ref, key, rel string, i int, steps []Step, made []map[string]*stepLines) (int, error) {
	m := stepRef.FindStringSubmatch(ref)
	if m == nil {
		return 0, fmt.Errorf("%s is %q: give a line number, or a reference to the lines an earlier step made, such as $prev.end, $2.start or $helper.end+1", key, ref)
	}
	name, edge, offset := m[1], m[2], m[3]
	j := -1 // the step referred to, 0-based
	switch n, err := strconv.Atoi(name); {
	case name == "prev":
		if i == 0 {
			return 0, fmt.Errorf("%s is %q, but there is no step before this one", key, ref)
		}
		j = i - 1
	case err == nil:
		if n < 1 || n > len(steps) {
			return 0, fmt.Errorf("%s is %q, but the batch has no step %d", key, ref, n)
		}
		j = n - 1
	default:
		for k, s := range steps {
			if s.ID == name {
				j = k
			}
		}
		if j < 0 || j >= i {
			return 0, fmt.Errorf("%s is %q, but no step before this one has id %q", key, ref, name)
		}
	}
	if j >= i {
		return 0, fmt.Errorf("%s is %q, but step %d has not run yet: a reference names an earlier step", key, ref, j+1)
	}
	s := made[j][rel]
	switch {
	case s == nil:
		return 0, fmt.Errorf("%s is %q, but step %d did not change %s", key, ref, j+1, rel)
	case s.places > 1:
		return 0, fmt.Errorf("%s is %q, but step %d changed %s in %d places, so it has no one range of lines; address the line by number, as it was before the batch, or by name", key, ref, j+1, rel, s.places)
	case s.rewrittenBy != 0:
		return 0, fmt.Errorf("%s is %q, but what step %d made was rewritten by step %d; refer to step %d instead", key, ref, j+1, s.rewrittenBy, s.rewrittenBy)
	}
	first, last := s.span()
	line := first
	if edge == "end" {
		line = last
	}
	if offset != "" {
		d, _ := strconv.Atoi(offset)
		line += d
	}
	if line < 1 {
		return 0, fmt.Errorf("%s is %q, which is line %d", key, ref, line)
	}
	return line, nil
}
