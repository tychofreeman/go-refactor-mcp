// Package addfield is add_field for TypeScript: an edit that adds
// properties to an interface, a class or an object type.
package addfield

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// AddField adds properties to a type.
type AddField struct{}

// New returns add_field.
func New() *AddField { return &AddField{} }

func (*AddField) ChangesBehavior() {}

func (*AddField) Name() string { return "add_field" }

func (*AddField) Description() string {
	return `Add properties to an interface, a class or an object type (type Spot = { ... }): an edit, to give a type the state a feature needs (add_property is another name for it in a batch). field "colour: string;" with after "height" adds colour just after height in interface Plant. field is TypeScript, one or more members with their doc comments (in a class, with initializers or methods too); with type, field gives only names (field "colour", type "string"). Without after or before they go at the end. A type on one line is opened up into lines.
Identify the type by file and name. Refused with the compiler's errors: a duplicate name, an object literal of the type that lacks a new required property.`
}

func (*AddField) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":   {"type": "string", "description": "Relative to the root."},
    "name":   {"type": "string", "description": "The interface, class or object type's name."},
    "field":  {"type": "string", "description": "The members to add, as TypeScript, e.g. \"timeout?: number; // per try\"."},
    "type":   {"type": "string", "description": "Optional: their type, when field gives only names (field \"owner\", type \"string\")."},
    "after":  {"type": "string", "description": "Optional: a member to put them just after."},
    "before": {"type": "string", "description": "Optional: a member to put them just before."}
  },
  "required": ["file", "name", "field"],
  "additionalProperties": false
}`)
}

func (*AddField) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File   string `json:"file"`
		Name   string `json:"name"`
		Field  string `json:"field"`
		Type   string `json:"type"`
		After  string `json:"after"`
		Before string `json:"before"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", a.File)
	}
	content := strings.TrimSpace(a.Field)
	if a.Type != "" {
		var lines []string
		for _, n := range strings.Split(content, ",") {
			lines = append(lines, strings.TrimSpace(n)+": "+strings.TrimSpace(a.Type)+ws.Semi(path))
		}
		content = strings.Join(lines, "\n")
	}
	pl, kind, why := ws.MemberPlace(path, a.Name, a.After, a.Before)
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	names, multiline, why := tsapi.ParseMembers(content, kind)
	switch {
	case why != "":
		return nil, refactor.Refusef("%s", why)
	case len(names) == 0:
		return nil, refactor.Refusef("field has no members")
	}
	text := pl.Prefix + gocode.Reindent(content, pl.Indent, multiline) + pl.Suffix
	return &refactor.Plan{
		Summary:  fmt.Sprintf("added %s to %s in %s", strings.Join(names, ", "), a.Name, ws.Rel(path)),
		Edits:    []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: pl.At, End: pl.End, NewText: text}}}},
		Supplied: content,
	}, nil
}
