// Package adddecl is add_declaration for TypeScript: an edit that adds
// declarations to a file, at its end or next to one it names.
package adddecl

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// AddDeclaration adds declarations to a file.
type AddDeclaration struct{}

// New returns add_declaration.
func New() *AddDeclaration { return &AddDeclaration{} }

func (*AddDeclaration) ChangesBehavior() {}

func (*AddDeclaration) Name() string { return "add_declaration" }

func (*AddDeclaration) Description() string {
	return `Add declarations (functions, classes, interfaces, types, variables, or members of a class) to an existing file: an edit; for a new file, use create_file. They go at the end of the file, or just after or before the declaration named in after or before: after "Bed.plant" puts a new method right after plant, inside Bed, so content is written as a member there.
Refuses, saying why: a name after or before does not find; content that does not type-check where it goes.`
}

func (*AddDeclaration) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":    {"type": "string", "description": "Relative to the root."},
    "content": {"type": "string", "description": "TypeScript declarations, with their doc comments."},
    "after":   {"type": "string", "description": "Optional: put them just after this declaration of file (name, or Class.member)."},
    "before":  {"type": "string", "description": "Optional: put them just before this declaration of file."}
  },
  "required": ["file", "content"],
  "additionalProperties": false
}`)
}

func (*AddDeclaration) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File    string `json:"file"`
		Content string `json:"content"`
		After   string `json:"after"`
		Before  string `json:"before"`
	}
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
	n := strings.Count(content, "\n") + 1
	return &refactor.Plan{
		Summary:  fmt.Sprintf("added %d line%s to %s", n, plural(n), ws.Rel(path)),
		Edits:    []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: at, End: at, NewText: insert}}}},
		Supplied: content,
	}, nil
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
