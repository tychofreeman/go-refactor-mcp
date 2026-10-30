// Package shapes has the TypeScript refactorings that change the shape
// of code across a file or the solution: fold, parameterize,
// merge_duplicate_function, remove_dead_code, extract_interface and
// union_switch_to_method.
package shapes

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

var bg = context.Background()

// plan runs change on the file's projects and returns the plan with the
// summary given.
func plan(ws *workspace.Workspace, file string, summary func() string, change func(p *tsapi.Project, path string) ([]tsapi.Edit, string)) (*refactor.Plan, error) {
	path := ws.Abs(file)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", file)
	}
	edits, why := ws.Edits(path, func(p *tsapi.Project) ([]tsapi.Edit, string) { return change(p, path) })
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	return &refactor.Plan{Summary: summary(), Edits: edits}, nil
}

// Fold replaces code that does what a function does with calls of it.
type Fold struct{}

// NewFold returns fold.
func NewFold() *Fold { return &Fold{} }

func (*Fold) Name() string { return "fold" }

func (*Fold) Description() string {
	return `Replace code that does what an existing function does with calls to it: every match of the function's body in the solution, its parameters (and this, for a method) standing for any expressions of their types, becomes a call. The function stays. extract_function makes a function from one place; fold reuses one, written or extracted, everywhere else.
What a body matches:
- One return: p.height * 2 + margin becomes tall(p, margin) for function tall(p: Plant, margin: number) { return p.height * 2 + margin; }; with a method, bed.depth * 2 becomes bed.twice() for twice() { return this.depth * 2; }.
- A parameter used only by its properties: feed(bed, n, n + 1) becomes feed2(bed, { plants: n, ml: n + 1 }) for function feed2(bed: string, d: Dose) { return feed(bed, d.plants, d.ml); }, to move callers to a parameter object.
- A setter, { ...p, f: v }: { name: n, height: 3 } of p's type becomes withHeight({ name: n }, 3).
- Statements, for a function without a result: the same statements in any block become one call, so near-duplicates become calls of one function you declare: log("water " + bed); count++ ... becomes note(bed) where note's body is those statements with bed its parameter. A string parameter also matches constant text of a template and a run of a + chain: with function record(entries: string[], device: string, bed: string, text: string) { log(` + "`" + `${device} on ${bed}` + "`" + `); entries.push(bed + ": " + text); }, log(` + "`" + `sprinkler on ${bed}` + "`" + `); this.entries.push(bed + ": " + note + "!"); becomes record(this.entries, "sprinkler", bed, note + "!").
Identify the function by file and name (name or Class.method); with line (and in), only the matches on that line are folded, to move callers over one at a time. A file that does not import the function gets the import.
A match where the call would not do the same (an argument evaluated in another order or another number of times, a name that means something else there, a function the file cannot import) is left, and the result says why.`
}

func (*Fold) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file": {"type": "string",  "description": "Path of the file that declares the function, relative to the root."},
    "name": {"type": "string",  "description": "The function: name, or Class.method for a method."},
    "line": {"type": "integer", "description": "Optional: fold only the matches that start on this line of file (or of in)."},
    "in":   {"type": "string",  "description": "Optional, with line: the file whose line it is, if not file."}
  },
  "required": ["file", "name"],
  "additionalProperties": false
}`)
}

func (*Fold) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File string `json:"file"`
		Name string `json:"name"`
		Line int    `json:"line"`
		In   string `json:"in"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	in := ""
	if a.In != "" {
		in = ws.Abs(a.In)
	}
	n := 0
	var skipped []string
	return plan(ws, a.File, func() string {
		s := fmt.Sprintf("folded %d match%s of %s into calls", n, map[bool]string{true: "", false: "es"}[n == 1], a.Name)
		for _, k := range skipped {
			s += "\nleft " + k
		}
		return s
	}, func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		es, folded, sk, why := p.Fold(bg, path, a.Name, a.Line, in)
		n, skipped = folded, sk
		return es, why
	})
}

// Parameterize makes functions that differ in literals calls of one.
type Parameterize struct{}

// NewParameterize returns parameterize.
func NewParameterize() *Parameterize { return &Parameterize{} }

func (*Parameterize) Name() string { return "parameterize" }

func (*Parameterize) Description() string {
	return `Make functions that are the same but for literals into calls of one new function that takes those literals as parameters: mist(bed: string) { log("mist on " + bed); } and drip(row: string) { log("drip on " + row); } become calls on(bed, "mist on ") and on(row, "drip on ") of function on(bed: string, what: string) { log(what + bed); }. Each keeps its signature and callers; inline_function with all_calls retires them.
Give file, functions (two or more of one file with the same signature; methods of one class), name for the new function, and parameters: a name for each literal that differs, in the order they appear.
Refuses, saying why: functions that differ in more than literals, or not at all (merge_duplicate_function is for that), a name already taken.`
}

func (*Parameterize) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":       {"type": "string", "description": "Path of the file declaring the functions, relative to the root."},
    "functions":  {"type": "array", "items": {"type": "string"}, "description": "The functions, name or Class.method; the first one's names are the new function's."},
    "name":       {"type": "string", "description": "Name of the new function (a method of the same class, for methods)."},
    "parameters": {"type": "array", "items": {"type": "string"}, "description": "A name for each literal that differs, in the order they appear in the first function."}
  },
  "required": ["file", "functions", "name", "parameters"],
  "additionalProperties": false
}`)
}

func (*Parameterize) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File       string   `json:"file"`
		Functions  []string `json:"functions"`
		Name       string   `json:"name"`
		Parameters []string `json:"parameters"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return plan(ws, a.File, func() string {
		return fmt.Sprintf("made %s calls of %s(%s)", strings.Join(a.Functions, ", "), a.Name, strings.Join(a.Parameters, ", "))
	}, func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		return p.Parameterize(bg, path, a.Functions, a.Name, a.Parameters)
	})
}

// MergeDuplicate replaces a function with an identical one.
type MergeDuplicate struct{}

// NewMergeDuplicate returns merge_duplicate_function.
func NewMergeDuplicate() *MergeDuplicate { return &MergeDuplicate{} }

func (*MergeDuplicate) Name() string { return "merge_duplicate_function" }

func (*MergeDuplicate) Description() string {
	return `Replace every use of a function with an identical one, and delete it: with function trim(s: string) { return s.trim(); } and function clean(t: string) { return t.trim(); }, merging trim into clean turns trim(x) into clean(x) everywhere, imports included, and deletes trim. Identical means the same signature and bodies that match node for node; parameters and locals may be named differently, every other name must mean the same. The refusal shows the first difference.
Identify the function to delete by file and name, or by line at its declaration or any use, and the one to keep by keep_file and keep_name.
Refuses, saying why: methods, generic functions or overloads, a function to keep that a file using the other cannot see (export it first), a re-export of the one to delete.`
}

func (*MergeDuplicate) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":      {"type": "string",  "description": "Path of a file containing the function to delete, at its declaration or a use, relative to the root."},
    "line":      {"type": "integer", "description": "1-based line of that occurrence. Omit it to find name at the top level of file."},
    "name":      {"type": "string",  "description": "The name of the function to delete."},
    "column":    {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "keep_file": {"type": "string",  "description": "The file declaring the function to keep."},
    "keep_name": {"type": "string",  "description": "The name of the function to keep."}
  },
  "required": ["file", "name", "keep_file", "keep_name"],
  "additionalProperties": false
}`)
}

func (*MergeDuplicate) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File     string `json:"file"`
		Line     int    `json:"line"`
		Name     string `json:"name"`
		Column   int    `json:"column"`
		KeepFile string `json:"keep_file"`
		KeepName string `json:"keep_name"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	keep := ws.Abs(a.KeepFile)
	return plan(ws, a.File, func() string { return fmt.Sprintf("merged %s into %s", a.Name, a.KeepName) }, func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		return p.MergeDuplicate(bg, path, a.Line, a.Name, a.Column, keep, a.KeepName)
	})
}

// RemoveDeadCode deletes declarations nothing uses.
type RemoveDeadCode struct{}

// NewRemoveDeadCode returns remove_dead_code.
func NewRemoveDeadCode() *RemoveDeadCode { return &RemoveDeadCode{} }

func (*RemoveDeadCode) Name() string { return "remove_dead_code" }

func (*RemoveDeadCode) Description() string {
	return `Delete a top-level declaration or a member of a class or interface that nothing uses, with the comments above it and the imports only it used: once the last call of oldLabel went, removing oldLabel deletes the function and its import of format; removing Plant.colour deletes the property once nothing reads or writes it. Exported code goes too, with a note that the module's API changed.
Identify it by file and name (name, Class.member or Interface.member), or by line at its declaration; also names more declarations of the file to remove with it, such as functions that only call each other.
Refuses, saying why: something still used (its own body aside, imports included); a member that implements or overrides one of a base class or interface; a value whose evaluation does something; a statement declaring several variables; a constructor.`
}

func (*RemoveDeadCode) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":   {"type": "string",  "description": "Path of the file declaring it, relative to the root."},
    "line":   {"type": "integer", "description": "1-based line of its declaration. Omit it to find name at the top level of file."},
    "name":   {"type": "string",  "description": "The declared name: with line, just the member's name; without line, the top-level name, Class.member or Interface.member."},
    "column": {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "also":   {"type": "array", "items": {"type": "string"}, "description": "Optional: more names of the file to remove with it (name or Class.member); uses among them do not count."}
  },
  "required": ["file", "name"],
  "additionalProperties": false
}`)
}

func (*RemoveDeadCode) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File     string   `json:"file"`
		Line     int      `json:"line"`
		Name     string   `json:"name"`
		Column   int      `json:"column"`
		Also     []string `json:"also"`
		Exported bool     `json:"exported"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	note := ""
	return plan(ws, a.File, func() string {
		s := "removed " + strings.Join(append([]string{a.Name}, a.Also...), ", ")
		if note != "" {
			s += "\nnote: " + note
		}
		return s
	}, func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		es, n, why := p.RemoveDeadCode(bg, path, a.Line, a.Name, a.Column, a.Also)
		note = n
		return es, why
	})
}

// ExtractInterface names members of a type as a new interface.
type ExtractInterface struct{}

// NewExtractInterface returns extract_interface.
func NewExtractInterface() *ExtractInterface { return &ExtractInterface{} }

func (*ExtractInterface) Name() string { return "extract_interface" }

func (*ExtractInterface) Description() string {
	return `Name some members of a type as a new interface, so that code can depend on just those. From an interface, the members move into the new one, which the old one extends, so every use keeps working: moving depth and width of interface Bed { depth: number; width: number; name: string } into Size declares interface Size { depth: number; width: number } and makes interface Bed extends Size { name: string }. From a type alias of an object type, the same with type Bed = Size & { name: string }. From a class, the new interface has the members' signatures (a method's parameters and result, an accessor as a property) and the class implements it: class Sprinkler implements Watering.
Identify the type by file and name, or by line at its declaration or a use; fields are the members, new_type the new interface's name, declared just before the type (exported with it).
Refuses, saying why: a generic type, a private, protected or static member of a class, a member sharing its line with another, a name already taken.`
}

func (*ExtractInterface) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":     {"type": "string",  "description": "Path of the file declaring the type, relative to the root."},
    "line":     {"type": "integer", "description": "1-based line of its declaration or a use. Omit it to find name at the top level of file."},
    "name":     {"type": "string",  "description": "The class, interface or type alias."},
    "column":   {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "fields":   {"type": "array", "items": {"type": "string"}, "description": "The members to name in the new interface."},
    "new_type": {"type": "string",  "description": "Name of the new interface."}
  },
  "required": ["file", "name", "fields", "new_type"],
  "additionalProperties": false
}`)
}

func (*ExtractInterface) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File    string   `json:"file"`
		Line    int      `json:"line"`
		Name    string   `json:"name"`
		Column  int      `json:"column"`
		Fields  []string `json:"fields"`
		NewType string   `json:"new_type"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return plan(ws, a.File, func() string {
		return fmt.Sprintf("named %s of %s as interface %s", strings.Join(a.Fields, ", "), a.Name, a.NewType)
	}, func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		return p.ExtractInterface(bg, path, a.Line, a.Name, a.Column, a.Fields, a.NewType)
	})
}

// UnionSwitchToMethod turns a switch on a union of classes into methods.
type UnionSwitchToMethod struct{}

// NewUnionSwitchToMethod returns union_switch_to_method.
func NewUnionSwitchToMethod() *UnionSwitchToMethod { return &UnionSwitchToMethod{} }

func (*UnionSwitchToMethod) Name() string { return "union_switch_to_method" }

func (*UnionSwitchToMethod) Description() string {
	return `Turn a switch on a union of classes into a method of each class: each case's body becomes that class's method, the subject becomes this, and the switch becomes one call. With type Plant = Fern | Cactus, where each class has a kind ("fern", "cactus"), and method water,
  function water(p: Plant, days: number): number { switch (p.kind) { case "fern": return days * p.mist; case "cactus": return 1; } }
becomes
  function water(p: Plant, days: number): number { return p.water(days); }
with water(days: number): number { return days * this.mist; } in Fern and water(days: number): number { return 1; } in Cactus. An instanceof chain (if (p instanceof Fern) ... else if (p instanceof Cactus) ...) works the same way. The locals a case uses become parameters. Every case must return, or none.
Identify the switch by file and line, or by function when it is that function's only one; method is the new method's name.
Refuses, saying why: a class of the union without a case, a default or a final else, a case that falls through, assigns a local it uses, or uses this; a subject whose evaluation does something; a method name a class already has.`
}

func (*UnionSwitchToMethod) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":     {"type": "string",  "description": "Path of the file with the switch, relative to the root."},
    "line":     {"type": "integer", "description": "1-based line on which the switch, or the first if, starts."},
    "function": {"type": "string",  "description": "Instead of line: the function (name or Class.method) whose only such switch it is."},
    "method":   {"type": "string",  "description": "Name of the new method."}
  },
  "required": ["file", "method"],
  "additionalProperties": false
}`)
}

func (*UnionSwitchToMethod) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File     string `json:"file"`
		Line     int    `json:"line"`
		Function string `json:"function"`
		Method   string `json:"method"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return plan(ws, a.File, func() string { return fmt.Sprintf("moved the switch's cases into method %s", a.Method) }, func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		return p.UnionSwitchToMethod(bg, path, a.Line, a.Function, a.Method)
	})
}

// PrepareSwitchForTypes prepares a switch for its cases to move into
// classes one at a time.
type PrepareSwitchForTypes struct{}

// NewPrepareSwitchForTypes returns prepare_switch_for_types.
func NewPrepareSwitchForTypes() *PrepareSwitchForTypes { return &PrepareSwitchForTypes{} }

func (*PrepareSwitchForTypes) Name() string { return "prepare_switch_for_types" }

func (*PrepareSwitchForTypes) Description() string {
	return `Prepare a switch for its cases to move into classes one at a time, each case becoming the method of a class that implements an interface: a subject that has the method runs it, and the switch handles the rest. No class implements it yet, so nothing changes until a case moves. With interface Species and method water,
  function water(p: Plant, days: number): number { switch (p.species) { case "fern": return days * 3; case "cactus": return 1; } return 0; }
becomes
  interface Species { water(days: number): number; }
  function water(p: Plant, days: number): number { if (typeof p.species === "object" && p.species !== null && "water" in p.species) { return (p.species as Species).water(days); } switch (p.species) { ... } return 0; }
The subject must be able to hold an object: widen it first (change_field_type to unknown). The interface is declared before the function, or gets the method when the file has it. The locals the cases use become the method's parameters. Every case must return, or none (then the switch goes in the else). Cases that share a body (case "moss": case "cactus":) are split, each with a copy, so that each moves alone.
Identify the switch by file and line, or by function when it is that function's only switch.
Refuses, saying why: a subject that holds no object or would be evaluated again with another value, an interface that already has the method, a case that uses this, leaves other than by return or assigns a local it uses, a shared body that declares something.`
}

func (*PrepareSwitchForTypes) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":      {"type": "string",  "description": "The file with the switch, relative to the root."},
    "line":      {"type": "integer", "description": "1-based line on which the switch starts."},
    "function":  {"type": "string",  "description": "Instead of line: the function (name or Class.method) whose only switch it is."},
    "interface": {"type": "string",  "description": "The interface: declared before the function if the file has none of that name."},
    "method":    {"type": "string",  "description": "Name of the interface's new method."}
  },
  "required": ["file", "interface", "method"],
  "additionalProperties": false
}`)
}

func (*PrepareSwitchForTypes) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File      string `json:"file"`
		Line      int    `json:"line"`
		Function  string `json:"function"`
		Interface string `json:"interface"`
		Method    string `json:"method"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return plan(ws, a.File, func() string {
		return fmt.Sprintf("prepared the switch for its cases to move into classes implementing %s.%s", a.Interface, a.Method)
	}, func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		return p.PrepareSwitchForTypes(bg, path, a.Line, a.Function, a.Interface, a.Method)
	})
}
