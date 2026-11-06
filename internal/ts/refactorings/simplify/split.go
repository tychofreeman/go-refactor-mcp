package simplify

import (
	"encoding/json"
	"fmt"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// Split is TypeScript's split: a variable given a new value into two
// variables, and a loop doing two independent things into two loops, as
// Go's split does, with the same arguments.
type Split struct{ refactor.Named[splitArgs] }

// NewSplit returns split.
func NewSplit() *Split { return &Split{Named: refactor.Called[splitArgs]("split")} }

func (*Split) Description() string {
	return `Split code that does two things into two, without changing what it does.
kind variable: a variable given a new value that does not depend on its old one becomes a new variable from there: in let n = beds.length; log(n); n = 3 * beds.length; log(n), new_name waterings at the assignment gives const waterings = 3 * beds.length and the uses after it waterings.
kind loop: a loop doing two independent things becomes two: for (const b of beds) { water += b.water; plants += b.plants.length } at the second statement becomes for (const b of beds) { water += b.water } then for (const b of beds) { plants += b.plants.length }.
Identify a variable by file, line (the assignment) and name; a loop by file, line (the for) and at (the second loop's first statement).
Refuses, saying why: an assignment that reads the old value or is in a block inside the variable's; a variable a closure refers to; a name already taken; loop parts that leave the loop, depend on each other or both have effects; a header the body changes or with effects, or a for...of over what is not known to be an array.`
}

// splitArgs are the arguments of split.
type splitArgs struct {
	Kind    string `json:"kind" desc:"variable, loop or phase."`
	File    string `json:"file" desc:"Path of the file, relative to the root."`
	Line    int    `json:"line" desc:"variable: the line of the assignment from which the variable is a new one; loop: the line of the for statement."`
	Name    string `json:"name,omitempty" desc:"variable: the variable's name."`
	NewName string `json:"new_name,omitempty" desc:"variable: the new variable's name."`
	At      int    `json:"at,omitempty" desc:"loop: the line of the first statement of the body that goes to the second loop."`
	Type    string `json:"type,omitempty" schema:"-"`
}

func (*Split) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a splitArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	switch a.Kind {
	case "variable":
		return plan(ws, a.File, fmt.Sprintf("split %s at line %d into %s", a.Name, a.Line, a.NewName), func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
			return p.SplitVariable(bg, path, a.Line, a.Name, a.NewName)
		})
	case "loop":
		return plan(ws, a.File, fmt.Sprintf("split the loop on line %d at line %d into two", a.Line, a.At), func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
			return p.SplitLoop(bg, path, a.Line, a.At)
		})
	case "phase":
		return nil, refactor.Refusef("kind phase is not built for TypeScript yet: extract_function the second phase, then convert_params_to_object")
	}
	return nil, refactor.Refusef("kind is variable, loop or phase, not %q", a.Kind)
}
