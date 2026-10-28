// Package addelement is add_element for TypeScript: an edit that adds an element to an object or array literal.
package addelement

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// AddElement is add_element.
type AddElement struct{}

// New returns add_element.
func New() *AddElement { return &AddElement{} }

func (*AddElement) ChangesBehavior() {}

func (*AddElement) Name() string { return "add_element" }

func (*AddElement) Description() string {
	return `Add an element to an object or array literal: an edit. At the line of const sizes = {, element fern: 3 adds that property at the end; after or before puts it next to the element with that key (a property's name, or an array element as written). It follows the literal's layout: on one line, or a line each, with a trailing comma when the literal has one.
Identify the literal by file and the line it starts on; give column when more than one starts there.`
}

func (*AddElement) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":    {"type": "string",  "description": "The file, relative to the root."},
    "line":    {"type": "integer", "description": "1-based line the literal starts on."},
    "column":  {"type": "integer", "description": "Optional 1-based column of its { or [, when more than one literal starts on the line."},
    "element": {"type": "string",  "description": "The element: key: value for an object, a value for an array."},
    "after":   {"type": "string",  "description": "Optional: put it after the element with this key."},
    "before":  {"type": "string",  "description": "Optional: put it before the element with this key."}
  },
  "required": ["file", "line", "element"],
  "additionalProperties": false
}`)
}

func (*AddElement) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File    string `json:"file"`
		Line    int    `json:"line"`
		Column  int    `json:"column"`
		Element string `json:"element"`
		After   string `json:"after"`
		Before  string `json:"before"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", a.File)
	}
	edits, why := ws.Edits(path, func(p *tsapi.Project) ([]tsapi.Edit, string) {
		return p.AddElement(path, a.Line, a.Column, a.Element, a.After, a.Before)
	})
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	return &refactor.Plan{Summary: fmt.Sprintf("added %s to the literal on line %d", a.Element, a.Line), Edits: edits}, nil
}
