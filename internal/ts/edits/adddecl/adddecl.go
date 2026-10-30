// Package adddecl is add_declaration for TypeScript: an edit that adds
// declarations to a file, at its end or next to one it names.
package adddecl

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// AddDeclaration adds declarations to a file.
type AddDeclaration struct {
	refactor.Named[addDeclarationArgs]
}

// New returns add_declaration.
func New() *AddDeclaration {
	return &AddDeclaration{Named: refactor.Called[addDeclarationArgs]("add_declaration")}
}

func (*AddDeclaration) ChangesBehavior() {}

func (*AddDeclaration) Description() string {
	return `Add declarations (functions, classes, interfaces, types, variables, or members of a class) to an existing file: an edit; for a new file, use create_file. They go at the end of the file, or just after or before the declaration named in after or before: after "Bed.plant" puts a new method right after plant, inside Bed, so content is written as a member there. A function or method named like a stub (a body of only throw new Error("not implemented"), as create_from_usage writes) in the file, or in that class, replaces the stub.
Refuses, saying why: a name after or before does not find; content that does not type-check where it goes.`
}

// addDeclarationArgs are the arguments of add_declaration.
type addDeclarationArgs struct {
	File    string `json:"file" desc:"Relative to the root."`
	Content string `json:"content" desc:"TypeScript declarations, with their doc comments."`
	After   string `json:"after,omitempty" desc:"Optional: put them just after this declaration of file (name, or Class.member)."`
	Before  string `json:"before,omitempty" desc:"Optional: put them just before this declaration of file."`
}

func (*AddDeclaration) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a addDeclarationArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.After != "" && a.Before != "" {
		return nil, refactor.Refusef("give after or before, not both")
	}
	path := ws.Abs(a.File)
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, refactor.Refusef("%s: no such file; for a new file, use create_file", a.File)
	}
	if !ws.Has(path) {
		return nil, refactor.Refusef("%s is in none of the solution's projects", a.File)
	}
	content := strings.TrimRight(a.Content, "\n")
	text := string(src)
	var at int
	var insert string
	switch name := a.After + a.Before; {
	case name == "":
		at = len(text)
		insert = "\n" + content + "\n"
		if !strings.HasSuffix(text, "\n") && text != "" {
			insert = "\n" + insert
		}
	default:
		var found bool
		var start, end int
		for _, d := range ws.Outline(path) {
			if d.Name == name {
				found, start, end = true, d.Start, d.End
				break
			}
		}
		if !found {
			return nil, refactor.Refusef("%s declares no %q; inspect the file to see what is there", a.File, name)
		}
		if a.After != "" {
			at = lineEnd(text, end)
			insert = "\n\n" + content
		} else {
			at = lineStart(text, start)
			insert = content + "\n\n"
		}
	}
	owner := ""
	if anchor := a.After + a.Before; strings.Contains(anchor, ".") {
		owner = anchor[:strings.Index(anchor, ".")]
	}
	edits, replaced := replaceStubs(ws.Outline(path), text, content, owner)
	n := strings.Count(content, "\n") + 1
	summary := fmt.Sprintf("added %d line%s to %s", n, plural(n), ws.Rel(path))
	if len(edits) == 0 {
		edits = []edit.TextEdit{{Start: at, End: at, NewText: insert}}
	} else {
		summary += ", replacing the stub of " + strings.Join(replaced, " and ")
	}
	return &refactor.Plan{
		Summary:  summary,
		Edits:    []edit.FileEdit{{Path: path, Edits: edits}},
		Supplied: content,
	}, nil
}

// stubBody is the body create_from_usage gives a stub.
var stubBody = regexp.MustCompile(`^\{\s*throw new Error\("not implemented"\);?\s*\}$`)

// replaceStubs returns the edits by which the functions content declares
// (the methods, when owner names the class it goes into) replace stubs of
// the same name in text: content takes the first stub's place, at its
// indentation, and the other stubs go. None when there is no such stub.
func replaceStubs(outline []tsapi.Decl, text, content, owner string) ([]edit.TextEdit, []string) {
	var edits []edit.TextEdit
	var replaced []string
	for _, fn := range tsapi.Functions(content, owner != "") {
		key := fn
		if owner != "" {
			key = owner + "." + fn
		}
		for _, d := range outline {
			if d.Name != key || (d.Kind != "function" && d.Kind != "method") || d.BlockEnd <= d.BlockStart || !stubBody.MatchString(text[d.BlockStart:d.BlockEnd]) {
				continue
			}
			from, to := lineStart(text, d.Start), lineStart(text, d.End+1)
			if len(edits) == 0 {
				from += len(text[from:]) - len(strings.TrimLeft(text[from:], " \t"))
				to = lineEnd(text, d.End)
				edits = append(edits, edit.TextEdit{Start: from, End: to, NewText: strings.TrimLeft(content, " \t")})
			} else {
				edits = append(edits, edit.TextEdit{Start: from, End: to})
			}
			replaced = append(replaced, key)
		}
	}
	slices.SortFunc(edits, func(a, b edit.TextEdit) int { return a.Start - b.Start })
	return edits, replaced
}

// lineStart is the offset of the start of line l (1-based) in text.
func lineStart(text string, l int) int {
	off := 0
	for range l - 1 {
		i := strings.IndexByte(text[off:], '\n')
		if i < 0 {
			return len(text)
		}
		off += i + 1
	}
	return off
}

// lineEnd is the offset of the end of line l, before its newline.
func lineEnd(text string, l int) int {
	off := lineStart(text, l)
	if i := strings.IndexByte(text[off:], '\n'); i >= 0 {
		return off + i
	}
	return len(text)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
