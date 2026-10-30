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
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// function is the arguments that name the function a tool changes.
type function struct {
	File string `json:"file" desc:"The file that declares the function, relative to the root."`
	Name string `json:"name" desc:"The function's name, Class.method for a method, or Class.constructor."`
}

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
Identify the function by file and name (Class.method for a method; Class.constructor, whose calls are the news of the class and the super calls of classes extending it). A method changes with its family: the interface method it implements (Iface.method names one), the class method it overrides, and every other implementation and override, with the calls through each. value must mean the same at every call (a name every call sees alike); before puts the parameter ahead of that one (default: last, before a rest parameter). A call passing fewer arguments gets undefined for those it skips. replace_variable makes a variable at a module's top level the body reads a parameter: parameter rate, value rate turns function cost(n: number) { return n * rate; } into cost(n: number, rate: number), and each call passes rate.
Refuses, saying why: a function used as a value (xs.map(f) passes an index), a call that spreads its arguments, an overload with too few parameters to take it where it goes (overloads change with the implementation), a class extending one without a constructor of its own, a name the body already reads, a value a call cannot see.`
}

func (*AddParameter) InputSchema() json.RawMessage {
	return refactor.Schema[addParameterArgs]()
}

// addParameterArgs are the arguments of add_parameter.
type addParameterArgs struct {
	function
	Parameter       string `json:"parameter" desc:"Name of the new parameter."`
	Type            string `json:"type" desc:"Its type, as TypeScript valid in the declaring file."`
	Value           string `json:"value" desc:"The expression every call passes for it, e.g. \"\" or null or DEFAULT_UNIT."`
	Before          string `json:"before,omitempty" desc:"Optional: an existing parameter; the new one goes just before it."`
	ReplaceVariable bool   `json:"replace_variable,omitempty" desc:"The parameter is named like a variable at a module's top level that the body reads, and takes its place. Calls not proven to pass the variable's value are edits, listed in a note."`
}

func (*AddParameter) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a addParameterArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	note := ""
	if a.ReplaceVariable {
		if path, p, why := ws.Project(a.File); why == "" {
			note = p.ReplacedNote(context.Background(), path, a.Name, a.Parameter, a.Value)
		}
	}
	ch := tsapi.SignatureChange{Add: &tsapi.NewParameter{Name: a.Parameter, Type: a.Type, Value: a.Value, ReplaceVariable: a.ReplaceVariable}, Before: a.Before}
	pl, err := plan(ws, a.File, a.Name, ch, fmt.Sprintf("added parameter %s to %s", a.Parameter, a.Name))
	if err == nil && note != "" {
		pl.Notes = append(pl.Notes, note)
	}
	return pl, err
}

// RemoveParameter removes a parameter the body does not read, and its
// argument at every call.
type RemoveParameter struct{}

// NewRemove returns remove_parameter.
func NewRemove() *RemoveParameter { return &RemoveParameter{} }

func (*RemoveParameter) Name() string { return "remove_parameter" }

func (*RemoveParameter) Description() string {
	return `Remove a parameter the body does not read, and its argument at every call in the solution: removing height, function prune(bed: string, height: number) becomes prune(bed: string), and prune("roses", 3) becomes prune("roses"). Use it last when taking a feature out: first change the body so it no longer reads the parameter.
With value, a parameter the body reads goes too, when every call passes that expression of this or the other parameters, the inverse of introduce_parameter: value "this.depth" makes describe(p: Plant, depth: number) { return p.name + depth; } describe(p: Plant) { return p.name + this.depth; }, and bed.describe(rose, bed.depth) bed.describe(rose).
Identify the function by file and name (Class.method for a method), and give the parameter. A method changes with its family: the interface method it implements (Iface.method names one), the class method it overrides, and every other implementation and override, with the calls through each. A dropped argument must plainly have no effects: a name, a literal, this, a property of one, or a function.
Refuses, saying why: a body that reads the parameter or arguments, a parameter property (constructor(public soil: string)), a function used as a value, a call that spreads its arguments, an argument that could have effects.`
}

func (*RemoveParameter) InputSchema() json.RawMessage {
	return refactor.Schema[removeArgs]()
}

func (*RemoveParameter) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a removeArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return planRemove(ws, a)
}

// removeArgs are remove_parameter's arguments.
type removeArgs struct {
	function
	Parameter string   `json:"parameter" desc:"Name of the parameter to remove."`
	Value     string   `json:"value,omitempty" desc:"Optional: what every call passes for it, written in the body's terms (this, the other parameters); the body's uses become this."`
	Also      []string `json:"also,omitempty" desc:"With value: more functions of the file (name or Class.method) whose parameter of that name goes in the same step, so that calls among them that pass it on count."`
}

// Expand makes a removal with value two steps for each function it names
// (name, then also): the body's uses of the parameter become value, then,
// once every body has, the parameter goes from each, as each call is shown
// to pass value. A call among them that passes on its own parameter then
// passes the value.
func (*RemoveParameter) Expand(ws *workspace.Workspace, raw json.RawMessage) ([]refactor.StepOf[*workspace.Workspace], error) {
	var a removeArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Value == "" {
		return nil, nil
	}
	names := append([]string{a.Name}, a.Also...)
	var write, remove []refactor.StepOf[*workspace.Workspace]
	for i, n := range names {
		b := a
		b.Name = n
		b.Also = append(append([]string{}, names[:i]...), names[i+1:]...)
		args, err := json.Marshal(b)
		if err != nil {
			return nil, err
		}
		write = append(write, refactor.StepOf[*workspace.Workspace]{Refactoring: writeValue{}, Args: args})
		remove = append(remove, refactor.StepOf[*workspace.Workspace]{Refactoring: removeShown{}, Args: args})
	}
	return append(write, remove...), nil
}

// writeValue is the first step of remove_parameter with value: an edit
// that writes value for each use of the parameter in the body. The step
// after it shows that this changed nothing.
type writeValue struct{}

func (writeValue) Name() string        { return "remove_parameter" }
func (writeValue) Description() string { return "" }
func (writeValue) InputSchema() json.RawMessage {
	return refactor.Schema[removeArgs]()
}
func (writeValue) ChangesBehavior() {}

func (writeValue) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a removeArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", a.File)
	}
	edits, why := ws.Edits(path, func(p *tsapi.Project) ([]tsapi.Edit, string) {
		return p.WriteValue(context.Background(), path, a.Name, a.Parameter, a.Value)
	})
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	return &refactor.Plan{Summary: fmt.Sprintf("wrote %s for %s in %s", a.Value, a.Parameter, a.Name), Edits: edits}, nil
}

// removeShown is the second step of remove_parameter with value: the
// parameter goes, now that the body no longer reads it, once every call
// is shown to pass value.
type removeShown struct{}

func (removeShown) Name() string        { return "remove_parameter" }
func (removeShown) Description() string { return "" }
func (removeShown) InputSchema() json.RawMessage {
	return refactor.Schema[removeArgs]()
}

func (removeShown) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a removeArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return planRemove(ws, a)
}

// planRemove removes the parameter a names.
func planRemove(ws *workspace.Workspace, a removeArgs) (*refactor.Plan, error) {
	if len(a.Also) > 0 && a.Value == "" {
		return nil, refactor.Refusef("also goes with value: the functions that pass the parameter on, whose parameter goes in the same step")
	}
	summary := fmt.Sprintf("removed parameter %s from %s", a.Parameter, a.Name)
	if a.Value != "" {
		summary += ", which every call passed as " + a.Value
	}
	return plan(ws, a.File, a.Name, tsapi.SignatureChange{Remove: a.Parameter, Value: a.Value, Also: a.Also}, summary)
}

// MoveParameter moves a parameter, and its argument at every call.
type MoveParameter struct{}

// NewMove returns move_parameter.
func NewMove() *MoveParameter { return &MoveParameter{} }

func (*MoveParameter) Name() string { return "move_parameter" }

func (*MoveParameter) Description() string {
	return `Move a parameter to another position, and its argument at every call in the solution: with parameter height and before bed, function prune(bed: string, height: number) becomes prune(height: number, bed: string), and prune("roses", 3) becomes prune(3, "roses").
Identify the function by file and name (Class.method for a method); before is the parameter it goes ahead of (default: last, before a rest parameter). A method changes with its family: the interface method it implements (Iface.method names one), the class method it overrides, and every other implementation and override, with the calls through each.
Refuses, saying why: a call where the new order of evaluation could matter (an argument that could have effects), a call that passes only some of the arguments it reorders, a rest parameter, a function used as a value.`
}

func (*MoveParameter) InputSchema() json.RawMessage {
	return refactor.Schema[moveParameterArgs]()
}

// moveParameterArgs are the arguments of move_parameter.
type moveParameterArgs struct {
	function
	Parameter string `json:"parameter" desc:"Name of the parameter to move."`
	Before    string `json:"before,omitempty" desc:"Optional: the parameter it should go just before. Default: the end."`
}

func (*MoveParameter) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a moveParameterArgs
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
	return refactor.Schema[introduceParameterArgs]()
}

// introduceParameterArgs are the arguments of introduce_parameter.
type introduceParameterArgs struct {
	File       string `json:"file" desc:"The file that declares the function, relative to the root."`
	Function   string `json:"function" desc:"The function whose body has the expression: name, Class.method or Class.constructor."`
	Expression string `json:"expression" desc:"Source text of the expression, e.g. \"p.height\"."`
	Parameter  string `json:"parameter" desc:"Name of the new parameter."`
}

func (*IntroduceParameter) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a introduceParameterArgs
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
Identify the function by file and name (Class.method for a method, Class.constructor). A method changes with its family, as for add_parameter.
Refuses, saying why: a body that reads arguments, a rest parameter, a parameter that is already destructured or is a parameter property, a function used as a value, a call that spreads its arguments.`
}

func (*ParamsToObject) InputSchema() json.RawMessage {
	return refactor.Schema[paramsToObjectArgs]()
}

// paramsToObjectArgs are the arguments of convert_params_to_object.
type paramsToObjectArgs struct {
	function
	TypeName string `json:"type_name,omitempty" desc:"Optional: declare the object's type as an interface of this name."`
}

func (*ParamsToObject) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a paramsToObjectArgs
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
Identify the function by file and name (Class.method for a method). value must plainly have no effects. A method changes with its family, as for add_parameter.
Refuses, saying why: a function that returns nothing (void), an async function or generator, a function used as a value, a call that spreads its arguments.`
}

func (*AddResult) InputSchema() json.RawMessage {
	return refactor.Schema[addResultArgs]()
}

// addResultArgs are the arguments of add_result.
type addResultArgs struct {
	function
	Field string `json:"field" desc:"Name of the new result in the object."`
	Type  string `json:"type" desc:"Its type, as TypeScript valid in the declaring file."`
	Value string `json:"value" desc:"The expression every return gives it, e.g. null."`
	Kept  string `json:"kept,omitempty" desc:"Optional: the name the old result keeps in the object. Default: value."`
}

func (*AddResult) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a addResultArgs
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
Identify the property by file and name as Type.property. values, narrowing, maps each old constant to a value of the new type: {"fern": "FERN"} turns species: "fern" and p.species === "fern" into species: FERN and p.species === FERN.
Refuses, saying why: a method or accessor, a type alias that is not an object type (change it with change_type).`
}

func (*ChangeFieldType) InputSchema() json.RawMessage {
	return refactor.Schema[changeFieldTypeArgs]()
}

// changeFieldTypeArgs are the arguments of change_field_type.
type changeFieldTypeArgs struct {
	File   string            `json:"file" desc:"The file that declares the type, relative to the root."`
	Name   string            `json:"name" desc:"The property as Type.property."`
	Type   string            `json:"type" desc:"Its new type, as TypeScript valid in the declaring file."`
	Values map[string]string `json:"values,omitempty" desc:"Optional: for each old constant (\"fern\", or as written), the new type's value, put wherever the property is given, assigned, compared or switched on with it, e.g. {\"fern\": \"FERN\"}."`
}

func (*ChangeFieldType) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a changeFieldTypeArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return retype(ws, a.File, func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		return p.ChangeFieldType(path, a.Name, a.Type, a.Values)
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
Identify the function by file and name (Class.method for a method). A method changes with its family, as for add_parameter.
Refuses, saying why: a function with overload signatures, a destructured parameter.`
}

func (*ChangeParameterType) InputSchema() json.RawMessage {
	return refactor.Schema[changeParameterTypeArgs]()
}

// changeParameterTypeArgs are the arguments of change_parameter_type.
type changeParameterTypeArgs struct {
	function
	Parameter string `json:"parameter" desc:"Name of the parameter whose type changes."`
	Type      string `json:"type" desc:"Its new type, as TypeScript valid in the declaring file."`
}

func (*ChangeParameterType) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a changeParameterTypeArgs
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
	return refactor.Schema[functionToMethodArgs]()
}

// functionToMethodArgs are the arguments of function_to_method.
type functionToMethodArgs struct {
	File    string `json:"file" desc:"The file that declares the function, relative to the root."`
	Name    string `json:"name" desc:"The function's name."`
	NewName string `json:"new_name,omitempty" desc:"Optional name for the method (default: the function's)."`
}

func (*FunctionToMethod) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a functionToMethodArgs
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
	return refactor.Schema[methodToFunctionArgs]()
}

// methodToFunctionArgs are the arguments of method_to_function.
type methodToFunctionArgs struct {
	File    string `json:"file" desc:"The file that declares the function, relative to the root."`
	Name    string `json:"name" desc:"The method as Class.method."`
	NewName string `json:"new_name,omitempty" desc:"Optional name for the function (default: the method's)."`
}

func (*MethodToFunction) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a methodToFunctionArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return retype(ws, a.File, func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		return p.MethodToFunction(context.Background(), path, a.Name, a.NewName)
	}, fmt.Sprintf("made %s a function", a.Name))
}

// MoveMethod moves methods down to a class that extends theirs.
type MoveMethod struct{}

// NewMoveMethod returns move_method.
func NewMoveMethod() *MoveMethod { return &MoveMethod{} }

func (*MoveMethod) Name() string { return "move_method" }

func (*MoveMethod) Description() string {
	return `Move a method down to a class that extends its class, when every use reaches it through that class: with class Bed extends Soil, moving Soil.acid to Bed takes acid() { return this.ph < 6; } out of Soil and puts it at the end of Bed, and b.acid() on a Bed stays. also names more methods of the class to move with it, such as those that call it on this.
To move a method to a parameter's type instead, use method_to_function, then function_to_method.
Refuses, saying why: a use through a value of another class (a Pot extends Soil too), a call on this in a method that does not move, a class to that does not extend the method's, a name to already has, a method that implements an interface's or overrides or is overridden, a static method.`
}

func (*MoveMethod) InputSchema() json.RawMessage {
	return refactor.Schema[moveMethodArgs]()
}

// moveMethodArgs are the arguments of move_method.
type moveMethodArgs struct {
	File string   `json:"file" desc:"The file that declares the method, relative to the root."`
	Name string   `json:"name" desc:"The method, as Class.method."`
	To   string   `json:"to" desc:"The class to move it to: one that extends Class."`
	Also []string `json:"also,omitempty" desc:"Other methods of the same class to move with it (method or Class.method)."`
}

func (*MoveMethod) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a moveMethodArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", a.File)
	}
	edits, why := ws.Edits(path, func(p *tsapi.Project) ([]tsapi.Edit, string) {
		return p.MoveMethod(context.Background(), path, a.Name, a.To, a.Also)
	})
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	return &refactor.Plan{Summary: fmt.Sprintf("moved %s to %s", strings.Join(append([]string{a.Name}, a.Also...), ", "), a.To), Edits: edits}, nil
}
