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
Identify the literal by file and the line it starts on (column when more than one starts there), or by file and name, the top-level variable it initializes.
from adds every element of another variable's literal, written by element as a template ($key, $value; copied without one): name "beds", from "soils", element "$key: { soil: $value }". from the literal itself rewrites each element.`
}

func (*AddElement) InputSchema() json.RawMessage {
	return refactor.Schema[addElementArgs]()
}

// addElementArgs are the arguments of add_element.
type addElementArgs struct {
	File    string `json:"file" desc:"The file, relative to the root."`
	Line    int    `json:"line,omitempty" desc:"1-based line the literal starts on; or give name."`
	Name    string `json:"name,omitempty" desc:"The top-level variable of the file the literal initializes; or give line."`
	From    string `json:"from,omitempty" desc:"Optional: a top-level variable whose literal's elements to add."`
	Column  int    `json:"column,omitempty" desc:"Optional 1-based column of its { or [, when more than one literal starts on the line."`
	Element string `json:"element,omitempty" desc:"The element: key: value for an object, a value for an array; with from, a template of $key and $value."`
	After   string `json:"after,omitempty" desc:"Optional: put it after the element with this key."`
	Before  string `json:"before,omitempty" desc:"Optional: put it before the element with this key."`
}

func (*AddElement) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a addElementArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	switch {
	case (a.Line == 0) == (a.Name == ""):
		return nil, refactor.Refusef("give line, where the literal starts, or name, the top-level variable it initializes")
	case a.Element == "" && a.From == "":
		return nil, refactor.Refusef("give element, or from")
	}
	path := ws.Abs(a.File)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", a.File)
	}
	edits, why := ws.Edits(path, func(p *tsapi.Project) ([]tsapi.Edit, string) {
		return p.AddElement(path, a.Line, a.Column, a.Name, a.From, a.Element, a.After, a.Before)
	})
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	what, where := a.Element, fmt.Sprintf("on line %d", a.Line)
	if a.From != "" {
		what = "the elements of " + a.From
	}
	if a.Name != "" {
		where = "of " + a.Name
	}
	return &refactor.Plan{Summary: fmt.Sprintf("added %s to the literal %s", what, where), Edits: edits}, nil
}
