// Package replacetext implements replace_text, an edit: it may change
// behavior.
package replacetext

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// ReplaceText is the "replace_text" edit.
type ReplaceText struct{}

// New returns the replace_text edit.
func New() *ReplaceText { return &ReplaceText{} }

func (*ReplaceText) Name() string { return "replace_text" }

func (*ReplaceText) Description() string {
	return `Replace exact text in a file with new text. This is an edit, not a refactoring: it may change behavior.
Use it for what the other tools do not cover: text inside string literals and comments, files that are not Go (go.mod, JSON, test fixtures), or a change the other edits make awkward. To restructure code, use the refactorings; to change Go code, replace_statements, replace_expression and insert_statements usually fit better.
Give file, old (exactly as in the file, whitespace and indentation included) and new. old must occur once, or set all_occurrences to replace every occurrence. It addresses text, not lines, so it stays valid after earlier steps of a batch.
Like every step, the result must type-check, or it is refused with the errors and nothing changes.`
}

func (*ReplaceText) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":            {"type": "string",  "description": "Path of the file, relative to the workspace root: any file of the module, Go or not."},
    "old":             {"type": "string",  "description": "The text to replace, exactly as it is in the file (whitespace and indentation too)."},
    "new":             {"type": "string",  "description": "The text to put in its place."},
    "all_occurrences": {"type": "boolean", "description": "Replace every occurrence of old in the file, not just the only one."}
  },
  "required": ["file", "old", "new"],
  "additionalProperties": false
}`)
}

func (*ReplaceText) ChangesBehavior() {}

// Plan checks preconditions and computes the edits.
func (*ReplaceText) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File           string `json:"file"`
		Old            string `json:"old"`
		New            string `json:"new"`
		AllOccurrences bool   `json:"all_occurrences"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.File == "" || a.Old == "" {
		return nil, refactor.Refusef("file and old are required")
	}
	path := ws.Abs(a.File)
	rel := ws.Rel(path)
	if !ws.Contains(path) {
		return nil, refactor.Refusef("%s is outside the workspace %s", a.File, ws.Root)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, refactor.Refusef("cannot read %s: %v; to make a new file, use create_file", rel, err)
	}
	var at []int // byte offsets of old in src
	for i := 0; ; {
		j := strings.Index(string(src[i:]), a.Old)
		if j < 0 {
			break
		}
		at = append(at, i+j)
		i += j + len(a.Old)
	}
	switch {
	case len(at) == 0:
		return nil, refactor.Refusef("old was not found in %s; it must match the file exactly, including whitespace and indentation", rel)
	case len(at) > 1 && !a.AllOccurrences:
		var lines []string
		for _, off := range at {
			lines = append(lines, strconv.Itoa(1+strings.Count(string(src[:off]), "\n")))
		}
		return nil, refactor.Refusef("old occurs %d times in %s (lines %s); include more of the surrounding text to pick one, or set all_occurrences", len(at), rel, strings.Join(lines, ", "))
	}
	var edits []edit.TextEdit
	for _, off := range at {
		edits = append(edits, edit.TextEdit{Start: off, End: off + len(a.Old), NewText: a.New})
	}
	return &refactor.Plan{
		Summary:  fmt.Sprintf("replaced %d occurrence%s of the text in %s", len(at), textutil.Plural(len(at)), rel),
		Edits:    []edit.FileEdit{{Path: path, Edits: edits}},
		Supplied: a.New,
	}, nil
}
