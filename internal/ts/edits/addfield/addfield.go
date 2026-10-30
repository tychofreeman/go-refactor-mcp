// Package addfield is add_field for TypeScript: an edit that adds
// properties to an interface, a class or an object type.
package addfield

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// AddField adds properties to a type.
type AddField struct{ refactor.Named[addFieldArgs] }

// New returns add_field.
func New() *AddField { return &AddField{Named: refactor.Called[addFieldArgs]("add_field")} }

func (*AddField) ChangesBehavior() {}

func (*AddField) Description() string {
	return `Add properties to an interface, a class or an object type (type Spot = { ... }): an edit, to give a type the state a feature needs (add_property is another name for it in a batch). field "colour: string;" with after "height" adds colour just after height in interface Plant. field is TypeScript, one or more members with their doc comments (in a class, with initializers or methods too); with type, field gives only names (field "colour", type "string"). Without after or before they go at the end. A type on one line is opened up into lines.
Identify the type by file and name. value gives one new property to each object literal of the type that lacks it: with field "colour: string;", value "'red'" adds colour: 'red' to each.
Refused with the compiler's errors: a duplicate name, an object literal of the type that lacks a new required property (give value, or make it optional).`
}

// addFieldArgs are the arguments of add_field.
type addFieldArgs struct {
	File   string `json:"file" desc:"Relative to the root."`
	Name   string `json:"name" desc:"The interface, class or object type's name."`
	Field  string `json:"field" desc:"The members to add, as TypeScript, e.g. \"timeout?: number; // per try\"."`
	Type   string `json:"type,omitempty" desc:"Optional: their type, when field gives only names (field \"owner\", type \"string\")."`
	After  string `json:"after,omitempty" desc:"Optional: a member to put them just after."`
	Before string `json:"before,omitempty" desc:"Optional: a member to put them just before."`
	Value  string `json:"value,omitempty" desc:"Optional, with one property: the expression each object literal of the type that lacks it gets for it."`
}

func (*AddField) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a addFieldArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path, p, why := ws.Project(a.File)
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	content := strings.TrimSpace(a.Field)
	if a.Type != "" {
		var lines []string
		for _, n := range strings.Split(content, ",") {
			lines = append(lines, strings.TrimSpace(n)+": "+strings.TrimSpace(a.Type)+p.Semi(path))
		}
		content = strings.Join(lines, "\n")
	}
	pl, kind, why := p.MemberPlace(path, a.Name, a.After, a.Before)
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
	plan := &refactor.Plan{
		Summary:  fmt.Sprintf("added %s to %s in %s", strings.Join(names, ", "), a.Name, ws.Rel(path)),
		Edits:    []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: pl.At, End: pl.End, NewText: text}}}},
		Supplied: content,
	}
	if a.Value != "" {
		if len(names) != 1 {
			return nil, refactor.Refusef("value goes with one property; add them one at a time")
		}
		fills, why := ws.FillLiterals(path, a.Name, names[0], a.Value)
		if why != "" {
			return nil, refactor.Refusef("%s", why)
		}
		n := 0
		for _, fe := range fills {
			n += len(fe.Edits)
		}
		plan.Edits = edit.Merge(append(plan.Edits, fills...))
		if n > 0 {
			plan.Notes = append(plan.Notes, fmt.Sprintf("gave %s %s in %d object literal%s", names[0], strings.TrimSpace(a.Value), n, map[bool]string{true: "", false: "s"}[n == 1]))
		}
	}
	return plan, nil
}
