// Package createfile is create_file for a TypeScript solution: an edit
// that adds a file, source or not.
package createfile

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// CreateFile creates a file.
type CreateFile struct{}

// New returns create_file.
func New() *CreateFile { return &CreateFile{} }

func (*CreateFile) ChangesBehavior() {}

func (*CreateFile) Name() string { return "create_file" }

func (*CreateFile) Description() string {
	return `Create a new file, TypeScript or not (a fixture, JSON), with any missing directories: an edit. A source file a project's include takes joins that project; what it adds must type-check, so a test written first goes with what it calls, in one batch with check "end".
The file must not exist; undo removes it.`
}

func (*CreateFile) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":    {"type": "string", "description": "Path of the new file, relative to the root."},
    "content": {"type": "string", "description": "The file's complete contents."}
  },
  "required": ["file", "content"],
  "additionalProperties": false
}`)
}

func (*CreateFile) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File    string `json:"file"`
		Content string `json:"content"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	if !ws.Contains(path) {
		return nil, refactor.Refusef("%s is outside the root %s, or in node_modules", a.File, ws.Root)
	}
	if _, err := os.Stat(path); err == nil {
		return nil, refactor.Refusef("%s already exists; change it with the other tools", a.File)
	}
	content := a.Content
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return &refactor.Plan{
		Summary:  fmt.Sprintf("created %s (%d lines)", ws.Rel(path), strings.Count(content, "\n")),
		Edits:    []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{NewText: content}}, Verbatim: true}},
		Supplied: content,
	}, nil
}
