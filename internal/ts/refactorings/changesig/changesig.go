// Package changesig changes the parameters of TypeScript functions and
// methods, at their declarations and at every call: add_parameter,
// remove_parameter, move_parameter, introduce_parameter,
// convert_params_to_object, add_result, function_to_method and
// method_to_function; and the edits of declared types change_field_type
// and change_parameter_type.
package changesig

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

const (
	fileProp = `"file": {"type": "string", "description": "The file that declares the function, relative to the root."}`
	nameProp = `"name": {"type": "string", "description": "The function's name, Class.method for a method, or Class.constructor."}`
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
Identify the function by file and name (Class.method for a method; Class.constructor, whose calls are the news of the class and the super calls of classes extending it). value must mean the same at every call (a name every call sees alike); before puts the parameter ahead of that one (default: last, before a rest parameter). A call passing fewer arguments gets undefined for those it skips.
Refuses, saying why: a function used as a value (xs.map(f) passes an index), a call that spreads its arguments, an overload with too few parameters to take it where it goes (overloads change with the implementation), a class extending one without a constructor of its own, a name the body already reads, a value a call cannot see.`
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
Refuses, saying why: a body that reads the parameter or arguments, a parameter property (constructor(public soil: string)), a function used as a value, a call that spreads its arguments, an argument that could have effects.`
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

// IntroduceParameter replaces an expression in a body with a new
// parameter, which every call passes the expression for.
type IntroduceParameter struct{}

// NewIntroduce returns introduce_parameter.
func NewIntroduce() *IntroduceParameter { return &IntroduceParameter{} }

func (*IntroduceParameter) Name() string { return "introduce_parameter" }

func (*IntroduceParameter) Description() string {
	return `Replace an expression in a function's body with a new parameter, and pass the expression at every call, written in the call's own terms: introducing p.height as height, function label(p: Plant): string { return p.name + p.height; } becomes label(p: Plant, height: number) with the body p.name + height, and label(rose) becomes label(rose, rose.height). Then remove_parameter p once the body no longer reads it. this works too in a method: this.depth in Bed.describe becomes bed.depth at bed.describe(rose).
Identify the expression by file, function (name, Class.method or Class.constructor) and its source text; every occurrence in the body that means the same is replaced. The parameter goes last, before a rest parameter; its type is the expression's.
The expression is then evaluated once, at the call, so it must have no effects and read nothing the body changes before it reads it. Refuses, saying why: a local of the function, a name a call sees otherwise, an argument that could have effects (it is evaluated twice), an expression in a loop or closure that reads changing state, a function used as a value.`
}

func (*IntroduceParameter) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + fileProp + `,
    "function":   {"type": "string", "description": "The function whose body has the expression: name, Class.method or Class.constructor."},
    "expression": {"type": "string", "description": "Source text of the expression, e.g. \"p.height\"."},
    "parameter":  {"type": "string", "description": "Name of the new parameter."}
  },
  "required": ["file", "function", "expression", "parameter"],
  "additionalProperties": false
}`)
}

func (*IntroduceParameter) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct{ File, Function, Expression, Parameter string }
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", a.File)
	}
	edits, why := ws.IntroduceParameter(path, a.Function, a.Expression, a.Parameter)
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	n := 0
	for _, fe := range edits {
		n += len(fe.Edits)
	}
	return &refactor.Plan{Summary: fmt.Sprintf("introduced parameter %s of %s for %s (%d edits in %d files)", a.Parameter, a.Function, a.Expression, n, len(edits)), Edits: edits}, nil
}

// ParamsToObject bundles a function's parameters into one object.
type ParamsToObject struct{}

// NewParamsToObject returns convert_params_to_object.
func NewParamsToObject() *ParamsToObject { return &ParamsToObject{} }

func (*ParamsToObject) Name() string { return "convert_params_to_object" }

func (*ParamsToObject) Description() string {
	return `Bundle a function's parameters into one object, destructured so that the body keeps its names, and pass an object literal at every call in the solution: function water(bed: string, liters = 2) becomes water({ bed, liters = 2 }: { bed: string; liters?: number }), and water("roses", 3) becomes water({ bed: "roses", liters: 3 }). With type_name, the object's type is an interface declared just before the function (WaterOptions). When every field is optional, the object defaults to {}.
Identify the function by file and name (Class.method for a method, Class.constructor).
Refuses, saying why: a body that reads arguments, a rest parameter, a parameter that is already destructured or is a parameter property, a function used as a value, a call that spreads its arguments.`
}

func (*ParamsToObject) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + fileProp + `,
    ` + nameProp + `,
    "type_name": {"type": "string", "description": "Optional: declare the object's type as an interface of this name."}
  },
  "required": ["file", "name"],
  "additionalProperties": false
}`)
}

func (*ParamsToObject) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File, Name string
		TypeName   string `json:"type_name"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", a.File)
	}
	edits, why := ws.ParamsToObject(path, a.Name, a.TypeName)
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	n := 0
	for _, fe := range edits {
		n += len(fe.Edits)
	}
	return &refactor.Plan{Summary: fmt.Sprintf("bundled the parameters of %s into an object (%d edits in %d files)", a.Name, n, len(edits)), Edits: edits}, nil
}

// AddResult adds a result to a function, which every call that uses the
// result leaves aside.
type AddResult struct{}

// NewAddResult returns add_result.
func NewAddResult() *AddResult { return &AddResult{} }

func (*AddResult) Name() string { return "add_result" }

func (*AddResult) Description() string {
	return `Add a result to a function or method (change_return_type is another name for it in a batch): its result becomes an object holding the old result and the new one, every return gives value for the new one, and every call that uses the result reads the old one: with field error, type string | null and value null, function load(id: string): string becomes load(id: string): { value: string; error: string | null }, return x becomes return { value: x, error: null }, and const v = load("a") becomes const v = load("a").value; a call as a statement stays. Then make the body return real errors. kept names the old result (default value).
Identify the function by file and name (Class.method for a method). value must plainly have no effects.
Refuses, saying why: a function that returns nothing (void), an async function or generator, a function used as a value, a call that spreads its arguments.`
}

func (*AddResult) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + fileProp + `,
    ` + nameProp + `,
    "field": {"type": "string", "description": "Name of the new result in the object."},
    "type":  {"type": "string", "description": "Its type, as TypeScript valid in the declaring file."},
    "value": {"type": "string", "description": "The expression every return gives it, e.g. null."},
    "kept":  {"type": "string", "description": "Optional: the name the old result keeps in the object. Default: value."}
  },
  "required": ["file", "name", "field", "type", "value"],
  "additionalProperties": false
}`)
}

func (*AddResult) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct{ File, Name, Field, Type, Value, Kept string }
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", a.File)
	}
	edits, why := ws.AddResult(path, a.Name, a.Field, a.Type, a.Value, a.Kept)
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	n := 0
	for _, fe := range edits {
		n += len(fe.Edits)
	}
	return &refactor.Plan{Summary: fmt.Sprintf("added result %s to %s (%d edits in %d files)", a.Field, a.Name, n, len(edits)), Edits: edits}, nil
}

// retype plans change, a change of a declared type, in the file named.
func retype(ws *workspace.Workspace, file string, change func(p *tsapi.Project, path string) ([]tsapi.Edit, string), summary string) (*refactor.Plan, error) {
	path := ws.Abs(file)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", file)
	}
	edits, why := ws.Edits(path, func(p *tsapi.Project) ([]tsapi.Edit, string) { return change(p, path) })
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	return &refactor.Plan{Summary: summary, Edits: edits}, nil
}

// ChangeFieldType changes a property's declared type.
type ChangeFieldType struct{}

// NewChangeFieldType returns change_field_type.
func NewChangeFieldType() *ChangeFieldType { return &ChangeFieldType{} }

// ChangesBehavior makes it an edit, checked with the rest of a batch:
// types are erased, so no value changes, but the uses are not adapted,
// and a batch with check "end" changes them after it.
func (*ChangeFieldType) ChangesBehavior() {}

func (*ChangeFieldType) Name() string { return "change_field_type" }

func (*ChangeFieldType) Description() string {
	return `Change the declared type of a property of a class, an interface or an object type: with name Plant.height and type Centimeters, height: number becomes height: Centimeters (a property without a type gets one). Types are erased, so no value changes, but the uses stay as they are: an edit, so that a batch with check "end" can change those that no longer type-check after it (an assertion such as p.height as number, or another annotation); alone, it is refused, listing them.
Identify the property by file and name as Type.property.
Refuses, saying why: a method or accessor, a type alias that is not an object type (change it with change_type).`
}

func (*ChangeFieldType) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file": {"type": "string", "description": "The file that declares the type, relative to the root."},
    "name": {"type": "string", "description": "The property as Type.property."},
    "type": {"type": "string", "description": "Its new type, as TypeScript valid in the declaring file."}
  },
  "required": ["file", "name", "type"],
  "additionalProperties": false
}`)
}

func (*ChangeFieldType) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct{ File, Name, Type string }
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return retype(ws, a.File, func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		return p.ChangeFieldType(path, a.Name, a.Type)
	}, fmt.Sprintf("changed the type of %s to %s", a.Name, a.Type))
}

// ChangeParameterType changes a parameter's declared type.
type ChangeParameterType struct{}

// NewChangeParameterType returns change_parameter_type.
func NewChangeParameterType() *ChangeParameterType { return &ChangeParameterType{} }

// ChangesBehavior makes it an edit, as ChangeFieldType's does.
func (*ChangeParameterType) ChangesBehavior() {}

func (*ChangeParameterType) Name() string { return "change_parameter_type" }

func (*ChangeParameterType) Description() string {
	return `Change the declared type of a function's or method's parameter: with name water, parameter liters and type Liters, function water(bed: string, liters: number) becomes water(bed: string, liters: Liters). Types are erased, so no value changes, but the body and the calls stay as they are: an edit, so that a batch with check "end" can change those that no longer type-check after it; alone, it is refused, listing them.
Identify the function by file and name (Class.method for a method).
Refuses, saying why: a function with overload signatures, a destructured parameter.`
}

func (*ChangeParameterType) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + fileProp + `,
    ` + nameProp + `,
    "parameter": {"type": "string", "description": "Name of the parameter whose type changes."},
    "type":      {"type": "string", "description": "Its new type, as TypeScript valid in the declaring file."}
  },
  "required": ["file", "name", "parameter", "type"],
  "additionalProperties": false
}`)
}

func (*ChangeParameterType) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct{ File, Name, Parameter, Type string }
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return retype(ws, a.File, func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		return p.ChangeParameterType(path, a.Name, a.Parameter, a.Type)
	}, fmt.Sprintf("changed the type of %s's parameter %s to %s", a.Name, a.Parameter, a.Type))
}

// FunctionToMethod makes a function a method of its first parameter's
// class.
type FunctionToMethod struct{}

// NewFunctionToMethod returns function_to_method.
func NewFunctionToMethod() *FunctionToMethod { return &FunctionToMethod{} }

func (*FunctionToMethod) Name() string { return "function_to_method" }

func (*FunctionToMethod) Description() string {
	return `Turn a function into a method of the class its first parameter is typed with, and change every call: function depth(b: Bed, extra: number): number becomes the method depth(extra: number): number at the end of Bed, with b as this, and depth(bed, 2) becomes bed.depth(2). The reverse of method_to_function. The class must be declared in the function's file: move_declaration the function there first.
Identify the function by file and name; new_name gives the method another name.
Refuses, saying why: a name the class, a class it extends or one extending it already has; a body that uses this, assigns the parameter or uses it in a nested function; a function used as a value, generic, overloaded or recursive.`
}

func (*FunctionToMethod) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + fileProp + `,
    "name":     {"type": "string", "description": "The function's name."},
    "new_name": {"type": "string", "description": "Optional name for the method (default: the function's)."}
  },
  "required": ["file", "name"],
  "additionalProperties": false
}`)
}

func (*FunctionToMethod) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File, Name string
		NewName    string `json:"new_name"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return retype(ws, a.File, func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		return p.FunctionToMethod(context.Background(), path, a.Name, a.NewName)
	}, fmt.Sprintf("made %s a method", a.Name))
}

// MethodToFunction makes a method a function taking the instance first.
type MethodToFunction struct{}

// NewMethodToFunction returns method_to_function.
func NewMethodToFunction() *MethodToFunction { return &MethodToFunction{} }

func (*MethodToFunction) Name() string { return "method_to_function" }

func (*MethodToFunction) Description() string {
	return `Turn a method into a function whose first parameter is the instance, and change every call: Bed's depth(extra: number): number becomes function depth(bed: Bed, extra: number): number after the class, with this as bed, and bed.depth(2) becomes depth(bed, 2); a file that calls it imports it. The reverse of function_to_method. Use it before moving a method elsewhere, or when it uses little of its class.
Identify the method by file and name (Class.method); new_name gives the function another name.
Refuses, saying why: a method that overrides or implements a member, or that a subclass overrides; a static method, a private name (#), a body using super; a method used as a value, called with ?. or calling itself; a name already taken.`
}

func (*MethodToFunction) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + fileProp + `,
    "name":     {"type": "string", "description": "The method as Class.method."},
    "new_name": {"type": "string", "description": "Optional name for the function (default: the method's)."}
  },
  "required": ["file", "name"],
  "additionalProperties": false
}`)
}

func (*MethodToFunction) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File, Name string
		NewName    string `json:"new_name"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return retype(ws, a.File, func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		return p.MethodToFunction(context.Background(), path, a.Name, a.NewName)
	}, fmt.Sprintf("made %s a function", a.Name))
}
