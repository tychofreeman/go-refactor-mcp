// Package replacetext is replace_text for a TypeScript solution: an edit
// of exact text in any file under the root.
package replacetext

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// ReplaceText replaces exact text.
type ReplaceText struct{}

// New returns replace_text.
func New() *ReplaceText { return &ReplaceText{} }

func (*ReplaceText) ChangesBehavior() {}

func (*ReplaceText) Name() string { return "replace_text" }

func (*ReplaceText) Description() string {
	return `Replace exact text in a file: an edit, for what the other tools do not cover: string literals, comments, and files that are not source (package.json, fixtures, docs). old "version 1.2" and new "version 1.3" change that text in README.md.
old must be exactly as in the file, whitespace and indentation included, and occur once, or set all_occurrences. files in place of file makes the same replacement in several files (old must be in each). It addresses text, not lines, so it stays valid after earlier steps of a batch.`
}

func (*ReplaceText) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":            {"type": "string",  "description": "Path of the file, relative to the root: any file under it, source or not."},
    "files":           {"type": "array", "items": {"type": "string"}, "description": "Instead of file: several files, to make the same replacement in each."},
    "old":             {"type": "string",  "description": "The text to replace, exactly as it is in the file (whitespace and indentation too)."},
    "new":             {"type": "string",  "description": "The text to put in its place."},
    "all_occurrences": {"type": "boolean", "description": "Replace every occurrence of old in the file, not just the only one."}
  },
  "required": ["old", "new"],
  "additionalProperties": false
}`)
}

func (*ReplaceText) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File  string   `json:"file"`
		Files []string `json:"files"`
		Old   string   `json:"old"`
		New   string   `json:"new"`
		All   bool     `json:"all_occurrences"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Old == "" {
		return nil, refactor.Refusef("old is empty; give the text to replace")
	}
	files := a.Files
	switch {
	case a.File != "" && len(a.Files) > 0:
		return nil, refactor.Refusef("give file or files, not both")
	case a.File != "":
		files = []string{a.File}
	case len(files) == 0:
		return nil, refactor.Refusef("give file, or files")
	}
	var fes []edit.FileEdit
	n := 0
	for _, f := range files {
		path := ws.Abs(f)
		if !ws.Contains(path) {
			return nil, refactor.Refusef("%s is outside the root %s, or in node_modules", f, ws.Root)
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, refactor.Refusef("%s: no such file", f)
		}
		text := string(src)
		count := strings.Count(text, a.Old)
		switch {
		case count == 0:
			return nil, refactor.Refusef("old was not found in %s; it must match the file exactly, whitespace and indentation included", f)
		case count > 1 && !a.All:
			return nil, refactor.Refusef("old occurs %d times in %s; make it longer to be unique, or set all_occurrences", count, f)
		}
		var tes []edit.TextEdit
		for off := 0; ; {
			i := strings.Index(text[off:], a.Old)
			if i < 0 {
				break
			}
			tes = append(tes, edit.TextEdit{Start: off + i, End: off + i + len(a.Old), NewText: a.New})
			off += i + len(a.Old)
		}
		n += len(tes)
		fes = append(fes, edit.FileEdit{Path: path, Edits: tes})
	}
	in := ws.Rel(ws.Abs(files[0]))
	if len(files) > 1 {
		in = fmt.Sprintf("%d files", len(files))
	}
	occ := "occurrence"
	if n != 1 {
		occ += "s"
	}
	return &refactor.Plan{Summary: fmt.Sprintf("replaced %d %s of the text in %s", n, occ, in), Edits: fes}, nil
}
