// Package inspect reads TypeScript as Go's inspect reads Go: an overview
// of the solution, a file without its function bodies, a function or
// lines with their numbers, declarations by name. What it cannot do yet
// (uses) it refuses, saying so.
package inspect

import (
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// Query is what to read; its fields are those of Go's inspect.
type Query struct {
	File      string   `json:"file,omitempty"`
	Function  string   `json:"function,omitempty"` // "name" or "Class.member"
	StartLine int      `json:"start_line,omitempty"`
	EndLine   int      `json:"end_line,omitempty"`
	Targets   []Query  `json:"targets,omitempty"`
	Names     []string `json:"names,omitempty"` // "name", "Class.member", or "file:name"
	Uses      []string `json:"uses,omitempty"`
}

// Options are the server's settings for reading.
type Options struct {
	// ByName shows source only for what a query names: a line range is
	// refused.
	ByName bool
}

func (o Options) part() string {
	if o.ByName {
		return "function"
	}
	return "function, or start_line/end_line"
}

// MaxLines is the most lines a file without its bodies is shown in; past
// it, the outline alone.
const MaxLines = 400

// RunWith answers q.
func RunWith(ws *workspace.Workspace, q Query, o Options) (string, error) {
	switch {
	case len(q.Uses) > 0:
		if q.File != "" || q.Function != "" || q.StartLine != 0 || q.EndLine != 0 || len(q.Targets) > 0 || len(q.Names) > 0 {
			return "", refactor.Refusef("give uses alone: each is found in the solution by its name, without file")
		}
		return ws.Uses(q.Uses), nil
	case len(q.Targets) > 0:
		return targets(ws, q, o)
	case len(q.Names) > 0:
		return names(ws, q.Names)
	case q.File == "":
		return overview(ws), nil
	case q.Function != "":
		return declarations(ws, q.File, []string{q.Function})
	}
	return file(ws, q, o)
}

// overview lists the solution's projects and, by directory, its files.
func overview(ws *workspace.Workspace) string {
	files := ws.Files()
	byDir := map[string][]string{}
	for _, f := range files {
		rel := ws.Rel(f)
		byDir[path.Dir(rel)] = append(byDir[path.Dir(rel)], path.Base(rel))
	}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	var sb strings.Builder
	fmt.Fprintf(&sb, "TypeScript solution at %s: %d files in %d projects (%s)\n", ws.Root, len(files), len(ws.Projects()), strings.Join(ws.Projects(), ", "))
	for _, d := range dirs {
		names := byDir[d]
		more := ""
		if len(names) > 10 {
			more = fmt.Sprintf(", and %d more", len(names)-10)
			names = names[:10]
		}
		fmt.Fprintf(&sb, "  %s  %s%s\n", d, strings.Join(names, ", "), more)
	}
	return sb.String()
}

// source returns the lines of the file at rel and its outline, or a
// refusal if no project of the solution has it.
func source(ws *workspace.Workspace, rel string) ([]string, []tsapi.Decl, error) {
	p := ws.Abs(rel)
	if !ws.Has(p) {
		return nil, nil, refactor.Refusef("file %s is in none of the solution's projects (call inspect without arguments to list files)", rel)
	}
	src, err := os.ReadFile(p)
	if err != nil {
		return nil, nil, err
	}
	lines := strings.SplitAfter(string(src), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines, ws.Outline(p), nil
}

func header(ws *workspace.Workspace, rel string, n int) string {
	return fmt.Sprintf("%s  (TypeScript, %d lines)\n", ws.Rel(ws.Abs(rel)), n)
}

// file renders q.File without its function bodies, or the lines q asks
// for.
func file(ws *workspace.Workspace, q Query, o Options) (string, error) {
	lines, decls, err := source(ws, q.File)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString(header(ws, q.File, len(lines)))
	if q.StartLine == 0 && q.EndLine == 0 {
		sk := skeleton(lines, decls)
		if len(sk) > MaxLines {
			sb.WriteString("\noutline:\n")
			for _, d := range decls {
				sb.WriteString(outlineLine(d))
			}
			fmt.Fprintf(&sb, "\nWithout its function bodies the file still has %d lines; pass %s to see source.\n", len(sk), o.part())
			return sb.String(), nil
		}
		fmt.Fprintf(&sb, "\nsource, function bodies elided (inspect with %s, to read one):\n", o.part())
		sb.WriteString(strings.Join(sk, ""))
		return sb.String(), nil
	}
	if o.ByName {
		return "", refactor.Refusef("lines by number are not shown here: read by name (function, or names)")
	}
	start, end := 1, len(lines)
	if q.StartLine > 0 {
		start = q.StartLine
	}
	if q.EndLine > 0 {
		end = q.EndLine
	}
	if start > end || start > len(lines) {
		return "", refactor.Refusef("invalid line range %d-%d; %s has %d lines", start, end, q.File, len(lines))
	}
	end = min(end, len(lines))
	fmt.Fprintf(&sb, "\nsource (lines %d-%d):\n", start, end)
	width := len(fmt.Sprint(end))
	for i := start; i <= end; i++ {
		sb.WriteString(numbered(width, i, lines[i-1]))
	}
	return sb.String(), nil
}

// skeleton returns lines, numbered, with each function's body elided: in
// its place a line says which lines it takes.
func skeleton(lines []string, decls []tsapi.Decl) []string {
	width := len(fmt.Sprint(len(lines)))
	pad := strings.Repeat(" ", width+6)
	byBody := map[int]tsapi.Decl{}
	for _, d := range decls {
		if d.BodyStart > 0 {
			byBody[d.BodyStart] = d
		}
	}
	var out []string
	for l := 1; l <= len(lines); l++ {
		if d, ok := byBody[l]; ok {
			out = append(out, fmt.Sprintf("%s… lines %d-%d   %s\n", pad, d.BodyStart, d.BodyEnd, facts(d)))
			l = d.BodyEnd
			continue
		}
		out = append(out, numbered(width, l, lines[l-1]))
	}
	return out
}

// facts is what is known of d beyond its source: its length.
func facts(d tsapi.Decl) string {
	if d.BodyStart == 0 {
		return ""
	}
	return fmt.Sprintf("[%d lines]", d.BodyEnd-d.BodyStart+1)
}

func outlineLine(d tsapi.Decl) string {
	f := facts(d)
	if f != "" {
		f = "   " + f
	}
	return fmt.Sprintf("  %4d-%-4d %s%s\n", d.Start, d.End, d.Signature, f)
}

func numbered(width, l int, line string) string {
	return fmt.Sprintf("%*d  %s\n", width, l, strings.TrimRight(line, "\n"))
}

// declarations renders file's header line, then the source of each
// declaration names gives, in that order.
func declarations(ws *workspace.Workspace, rel string, names []string) (string, error) {
	lines, decls, err := source(ws, rel)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString(header(ws, rel, len(lines)))
	for i, name := range names {
		d := find(decls, name)
		if d == nil {
			return "", refactor.Refusef("no declaration %q in %s; inspect the file to see what is there", name, rel)
		}
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(outlineLine(*d))
		fmt.Fprintf(&sb, "\nsource (lines %d-%d):\n", d.Start, d.End)
		width := len(fmt.Sprint(d.End))
		for l := d.Start; l <= d.End; l++ {
			sb.WriteString(numbered(width, l, lines[l-1]))
		}
	}
	return sb.String(), nil
}

func find(decls []tsapi.Decl, name string) *tsapi.Decl {
	for i := range decls {
		if decls[i].Name == name {
			return &decls[i]
		}
	}
	return nil
}

// names renders each declaration names gives, found in every file of the
// solution: "name", "Class.member", or "file:name" for one several files
// declare.
func names(ws *workspace.Workspace, names []string) (string, error) {
	var parts []string
	for _, n := range names {
		file, name, qualified := strings.Cut(n, ":")
		if !qualified {
			name, file = n, ""
			var in []string
			for _, f := range ws.Files() {
				if find(ws.Outline(f), name) != nil {
					in = append(in, ws.Rel(f))
				}
			}
			switch len(in) {
			case 0:
				return "", refactor.Refusef("no file of the solution declares %q", name)
			case 1:
				file = in[0]
			default:
				var q []string
				for _, f := range in {
					q = append(q, f+":"+name)
				}
				return "", refactor.Refusef("%q is declared in %d files; name one: %s", name, len(in), strings.Join(q, ", "))
			}
		}
		out, err := declarations(ws, file, []string{name})
		if err != nil {
			return "", err
		}
		parts = append(parts, out)
	}
	return strings.Join(parts, "\n"), nil
}

// targets renders each of q's targets in turn; one that is refused says
// so in its place.
func targets(ws *workspace.Workspace, q Query, o Options) (string, error) {
	if q.File != "" || q.Function != "" || q.StartLine != 0 || q.EndLine != 0 {
		return "", refactor.Refusef("give either targets or file (with function or start_line/end_line), not both")
	}
	var parts []string
	for i, t := range q.Targets {
		if len(t.Targets) > 0 {
			return "", refactor.Refusef("target %d has targets of its own; give one flat list", i+1)
		}
		out, err := RunWith(ws, t, o)
		switch {
		case refactor.IsRefusal(err):
			out = fmt.Sprintf("target %d: %v\n", i+1, err)
		case err != nil:
			return "", err
		}
		parts = append(parts, out)
	}
	return strings.Join(parts, "\n"), nil
}
