// Package changesig changes the parameters of TypeScript functions and
// methods, at their declarations and at every call: add_parameter,
// remove_parameter and move_parameter.
package changesig

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

const (
	fileProp = `"file": {"type": "string", "description": "The file that declares the function, relative to the root."}`
	nameProp = `"name": {"type": "string", "description": "The function's name, or Class.method for a method."}`
)

// plan makes ch to the function a names, with the summary given.
func plan(ws *workspace.Workspace, file, name string, ch tsapi.SignatureChange, summary string) (*refactor.Plan, error) {
	path := ws.Abs(file)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", file)
	}
	edits, why := ws.ChangeSignature(path, name, ch)
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	calls := 0
	for _, fe := range edits {
		calls += len(fe.Edits)
	}
	return &refactor.Plan{Summary: fmt.Sprintf("%s (%d edits in %d files)", summary, calls, len(edits)), Edits: edits}, nil
}

// AddParameter adds a parameter, and its value at every call.
type AddParameter struct{}

// NewAdd returns add_parameter.
func NewAdd() *AddParameter { return &AddParameter{} }

func (*AddParameter) Name() string { return "add_parameter" }

func (*AddParameter) Description() string {
	return `Add a parameter to a function or method, and pass the same value for it at every call in the solution: with parameter unit, type string and value "L", function water(bed: string, liters: number) becomes water(bed: string, liters: number, unit: string), and water("roses", 2) becomes water("roses", 2, "L"). The body does not use it yet, so behavior is unchanged; add it, then change the body.
Identify the function by file and name (Class.method for a method). value must mean the same at every call (a name every call sees alike); before puts the parameter ahead of that one (default: last, before a rest parameter). A call passing fewer arguments gets undefined for those it skips.
Refuses, saying why: a function used as a value (xs.map(f) passes an index), a call that spreads its arguments, overloads, a constructor, a name the body already reads, a value a call cannot see.`
}

func (*AddParameter) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + fileProp + `,
    ` + nameProp + `,
    "parameter": {"type": "string", "description": "Name of the new parameter."},
    "type":      {"type": "string", "description": "Its type, as TypeScript valid in the declaring file."},
    "value":     {"type": "string", "description": "The expression every call passes for it, e.g. \"\" or null or DEFAULT_UNIT."},
    "before":    {"type": "string", "description": "Optional: an existing parameter; the new one goes just before it."}
  },
  "required": ["file", "name", "parameter", "type", "value"],
  "additionalProperties": false
}`)
}

func (*AddParameter) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct{ File, Name, Parameter, Type, Value, Before string }
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	ch := tsapi.SignatureChange{Add: &tsapi.NewParameter{Name: a.Parameter, Type: a.Type, Value: a.Value}, Before: a.Before}
	return plan(ws, a.File, a.Name, ch, fmt.Sprintf("added parameter %s to %s", a.Parameter, a.Name))
}

// RemoveParameter removes a parameter the body does not read, and its
// argument at every call.
type RemoveParameter struct{}

// NewRemove returns remove_parameter.
func NewRemove() *RemoveParameter { return &RemoveParameter{} }

func (*RemoveParameter) Name() string { return "remove_parameter" }

func (*RemoveParameter) Description() string {
	return `Remove a parameter the body does not read, and its argument at every call in the solution: removing height, function prune(bed: string, height: number) becomes prune(bed: string), and prune("roses", 3) becomes prune("roses"). Use it last when taking a feature out: first change the body so it no longer reads the parameter.
Identify the function by file and name (Class.method for a method), and give the parameter. A dropped argument must plainly have no effects: a name, a literal, this, a property of one, or a function.
Refuses, saying why: a body that reads the parameter or arguments, a function used as a value, a call that spreads its arguments, an argument that could have effects.`
}

func (*RemoveParameter) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + fileProp + `,
    ` + nameProp + `,
    "parameter": {"type": "string", "description": "Name of the parameter to remove."}
  },
  "required": ["file", "name", "parameter"],
  "additionalProperties": false
}`)
}

func (*RemoveParameter) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct{ File, Name, Parameter string }
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return plan(ws, a.File, a.Name, tsapi.SignatureChange{Remove: a.Parameter}, fmt.Sprintf("removed parameter %s from %s", a.Parameter, a.Name))
}

// MoveParameter moves a parameter, and its argument at every call.
type MoveParameter struct{}

// NewMove returns move_parameter.
func NewMove() *MoveParameter { return &MoveParameter{} }

func (*MoveParameter) Name() string { return "move_parameter" }

func (*MoveParameter) Description() string {
	return `Move a parameter to another position, and its argument at every call in the solution: with parameter height and before bed, function prune(bed: string, height: number) becomes prune(height: number, bed: string), and prune("roses", 3) becomes prune(3, "roses").
Identify the function by file and name (Class.method for a method); before is the parameter it goes ahead of (default: last, before a rest parameter).
Refuses, saying why: a call where the new order of evaluation could matter (an argument that could have effects), a call that passes only some of the arguments it reorders, a rest parameter, a function used as a value.`
}

func (*MoveParameter) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + fileProp + `,
    ` + nameProp + `,
    "parameter": {"type": "string", "description": "Name of the parameter to move."},
    "before":    {"type": "string", "description": "Optional: the parameter it should go just before. Default: the end."}
  },
  "required": ["file", "name", "parameter"],
  "additionalProperties": false
}`)
}

func (*MoveParameter) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct{ File, Name, Parameter, Before string }
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	where := "the end"
	if a.Before != "" {
		where = "before " + a.Before
	}
	return plan(ws, a.File, a.Name, tsapi.SignatureChange{Move: a.Parameter, Before: a.Before}, fmt.Sprintf("moved parameter %s of %s to %s", a.Parameter, a.Name, where))
}
