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
	return `Add declarations (functions, classes, interfaces, types, variables, or members of a class) to an existing file; for a new file, use create_file. They are placed by kind: at the end of the file, or, for a const, let, class or enum a top-level statement already uses, just before it; with type "Bed", content is members of Bed: a field after its fields, a constructor after its constructors or fields, a method at its end. A function or method named like a stub (a body of only throw new Error("not implemented"), as create_from_usage writes) in the file, or in that class, replaces the stub. With test_for in place of content, a red test at the end of the file: test_for "Bed.plant" adds test("Bed.plant", ...), which makes a Bed, calls plant with placeholder arguments and throws, showing the result, until the assertion is written; test comes from vitest or jest when package.json names one, else node:test.`
}

// addDeclarationArgs are the arguments of add_declaration.
type addDeclarationArgs struct {
	File    string `json:"file" desc:"Relative to the root."`
	Content string `json:"content,omitempty" desc:"TypeScript declarations, with their doc comments."`
	TestFor string `json:"test_for,omitempty" desc:"Instead of content: the function or Class.method to write a red test for, at the end of file, with test imported from the solution's framework."`
	Type    string `json:"type,omitempty" desc:"Optional: the class content is members of; they go in it by kind."`
	After   string `json:"after,omitempty" schema:"-"`  // retired: placement is by kind; a Class. prefix still names type
	Before  string `json:"before,omitempty" schema:"-"` // retired, as After
}

func (*AddDeclaration) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a addDeclarationArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, refactor.Refusef("%s: no such file; for a new file, use create_file", a.File)
	}
	if !ws.Has(path) {
		return nil, refactor.Refusef("%s is in none of the solution's projects", a.File)
	}
	var frameworkImport string
	if a.TestFor != "" {
		if a.Content != "" {
			return nil, refactor.Refusef("give content or test_for, not both: test_for writes the test")
		}
		test, module, why := ws.TestFor(a.TestFor)
		if why != "" {
			return nil, refactor.Refusef("%s", why)
		}
		a.Content = test
		if module != "" && !strings.Contains(string(src), `from "`+module+`"`) {
			frameworkImport = `import { test } from "` + module + `";` + "\n"
		}
	}
	content := strings.TrimRight(a.Content, "\n")
	text := string(src)
	owner := a.Type
	if anchor := a.After + a.Before; owner == "" && strings.Contains(anchor, ".") {
		owner = anchor[:strings.Index(anchor, ".")]
	}
	edits, replaced := replaceStubs(ws.Outline(path), text, content, owner)
	n := strings.Count(content, "\n") + 1
	summary := fmt.Sprintf("added %d line%s to %s", n, plural(n), ws.Rel(path))
	if len(edits) == 0 {
		placed, why := tsapi.PlaceDeclarations(text, content, owner)
		if why != "" {
			return nil, refactor.Refusef("%s", why)
		}
		for _, p := range placed {
			edits = append(edits, edit.TextEdit{Start: p.Offset, End: p.Offset, NewText: p.Text})
		}
	} else {
		summary += ", replacing the stub of " + strings.Join(replaced, " and ")
	}
	if frameworkImport != "" {
		edits = append([]edit.TextEdit{{Start: 0, End: 0, NewText: frameworkImport}}, edits...)
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
