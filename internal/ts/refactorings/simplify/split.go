package simplify

import (
	"encoding/json"
	"fmt"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/refactorings/changesig"
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
kind phase: what the statements before line compute for the rest goes into an object of a new type, and the rest becomes the function new_name of it: const litres = b.rows * 2; const total = litres * days; return total * rate at the second line, with type Usage and new_name price, gives return price({ litres }, days, rate) and function price({ litres }: Usage, days: number, rate: number).
Identify a variable by file, line (the assignment) and name; a loop by file, line (the for) and at (the second loop's first statement); a phase by file and line (its first statement).`
}

// splitArgs are the arguments of split.
type splitArgs struct {
	Kind    string `json:"kind" desc:"variable, loop or phase."`
	File    string `json:"file" desc:"Path of the file, relative to the root."`
	Line    int    `json:"line" desc:"variable: the line of the assignment from which the variable is a new one; loop: the line of the for statement; phase: the line of the second phase's first statement."`
	Name    string `json:"name,omitempty" desc:"variable: the variable's name."`
	NewName string `json:"new_name,omitempty" desc:"variable: the new variable's name; phase: the second phase's new function."`
	At      int    `json:"at,omitempty" desc:"loop: the line of the first statement of the body that goes to the second loop."`
	Type    string `json:"type,omitempty" desc:"phase: the new type (an interface) of what the first phase computes."`
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
		return nil, refactor.Refusef("kind phase runs as two steps (Expand), not here")
	}
	return nil, refactor.Refusef("kind is variable, loop or phase, not %q", a.Kind)
}

// Expand makes kind phase two steps, as Go's split does: extract_function
// makes the second phase a function, then convert_params_to_object bundles
// what it takes from the first phase into an object of the new type.
func (*Split) Expand(ws *workspace.Workspace, raw json.RawMessage) ([]refactor.StepOf[*workspace.Workspace], error) {
	var a splitArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Kind != "phase" {
		return nil, nil
	}
	if a.NewName == "" || a.Type == "" {
		return nil, refactor.Refusef("kind phase takes new_name, the second phase's function, and type, the object of what the first phase computes")
	}
	path, p, why := ws.Project(a.File)
	var last int
	var names []string
	if why == "" {
		last, names, why = p.PhaseAt(bg, path, a.Line, a.Type)
	}
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	extract, err := json.Marshal(map[string]any{"file": a.File, "start_line": a.Line, "end_line": last, "name": a.NewName})
	if err != nil {
		return nil, err
	}
	bundle, err := json.Marshal(map[string]any{"file": a.File, "name": a.NewName, "parameters": names, "type_name": a.Type})
	if err != nil {
		return nil, err
	}
	return []refactor.StepOf[*workspace.Workspace]{{Refactoring: NewExtractFunction(), Args: extract}, {Refactoring: changesig.NewParamsToObject(), Args: bundle}}, nil
}
