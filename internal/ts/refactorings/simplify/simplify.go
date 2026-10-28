// Package simplify has the TypeScript refactorings that simplify code in
// place: extract_variable, inline_variable, move_statement_up and _down,
// invert_if, remove_else, extract_function and inline_function.
package simplify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

var bg = context.Background()

const fileProp = `"file": {"type": "string", "description": "Path of the file, relative to the root."}`

// plan runs change on the file's projects and returns the plan with the
// summary given.
func plan(ws *workspace.Workspace, file, summary string, change func(p *tsapi.Project, path string) ([]tsapi.Edit, string)) (*refactor.Plan, error) {
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

// ExtractVariable names an expression.
type ExtractVariable struct{}

// NewExtractVariable returns extract_variable.
func NewExtractVariable() *ExtractVariable { return &ExtractVariable{} }

func (*ExtractVariable) Name() string { return "extract_variable" }

func (*ExtractVariable) Description() string {
	return `Name an expression: const name = expression; is inserted just before its statement and the name used in its place. Extracting p.height * 2 as tall, return p.height * 2 + margin becomes const tall = p.height * 2; return tall + margin;
Identify the expression by file, the line it starts on and its source text (column if the text is on the line twice). all_occurrences also replaces the same expression later in the block, where nothing in between could change it.
Refuses, saying why: an expression evaluated only sometimes or more than once whose evaluation could then differ (after && or ?., in a loop's condition), one that would then run before something in its statement it must follow, one assigned to, a method called through it (its this), one in no block (an arrow's expression body), a name it uses that its statement declares.`
}

func (*ExtractVariable) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + fileProp + `,
    "line":            {"type": "integer", "description": "1-based line on which the expression starts."},
    "expression":      {"type": "string",  "description": "Source text of the expression, e.g. \"p.height * 2\"."},
    "name":            {"type": "string",  "description": "Name for the new constant."},
    "column":          {"type": "integer", "description": "Optional 1-based column where the expression starts, only needed if the text occurs more than once on the line."},
    "all_occurrences": {"type": "boolean", "description": "Also replace identical occurrences later in the same block."}
  },
  "required": ["file", "line", "expression", "name"],
  "additionalProperties": false
}`)
}

func (*ExtractVariable) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File       string `json:"file"`
		Line       int    `json:"line"`
		Expression string `json:"expression"`
		Name       string `json:"name"`
		Column     int    `json:"column"`
		All        bool   `json:"all_occurrences"`
		Pointer    bool   `json:"pointer"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Pointer {
		return nil, refactor.Refusef("pointer is for Go; TypeScript has no pointers")
	}
	n := 0
	p, err := plan(ws, a.File, "", func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		es, why := p.ExtractVariable(bg, path, a.Line, a.Expression, a.Column, a.Name, a.All)
		n = len(es) - 1
		return es, why
	})
	if err != nil {
		return nil, err
	}
	p.Summary = fmt.Sprintf("extracted %s as %s (%d occurrence%s)", a.Expression, a.Name, n, plural(n))
	return p, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// InlineVariable replaces a variable's uses with its value.
type InlineVariable struct{}

// NewInlineVariable returns inline_variable.
func NewInlineVariable() *InlineVariable { return &InlineVariable{} }

func (*InlineVariable) Name() string { return "inline_variable" }

func (*InlineVariable) Description() string {
	return `Replace every use of a local variable or constant with its value, and delete the declaration: the inverse of extract_variable. const tall = p.height * 2; return tall + margin; becomes return p.height * 2 + margin; A declaration of several variables keeps the others.
Identify the variable by file, line and name at its declaration or any use, or by file, function ("name" or "Class.method") and name, which stays valid later in a batch.
Refuses, saying why: a variable assigned after its declaration, exported, or declared by a pattern; a value that would be evaluated a different number of times or in another order with what lies between (one with calls needs a single use, with nothing in between it must not pass); a use inside a function, unless the value is constant; a name in the value that means something else at a use.`
}

func (*InlineVariable) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + fileProp + `,
    "line":     {"type": "integer", "description": "1-based line of the variable's declaration or of any use."},
    "name":     {"type": "string",  "description": "The variable's name."},
    "function": {"type": "string",  "description": "Instead of line: the function (name or Class.method) of file that declares the variable."},
    "column":   {"type": "integer", "description": "Optional 1-based column, only needed if the name occurs more than once on the line with different meanings."}
  },
  "required": ["file", "name"],
  "additionalProperties": false
}`)
}

func (*InlineVariable) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File     string `json:"file"`
		Line     int    `json:"line"`
		Name     string `json:"name"`
		Function string `json:"function"`
		Column   int    `json:"column"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	n := 0
	p, err := plan(ws, a.File, "", func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		es, why := p.InlineVariable(bg, path, a.Line, a.Function, a.Name, a.Column)
		n = len(es) - 1
		return es, why
	})
	if err != nil {
		return nil, err
	}
	p.Summary = fmt.Sprintf("inlined %s (%d use%s)", a.Name, n, plural(n))
	return p, nil
}

// MoveStatement swaps a statement with its neighbor.
type MoveStatement struct{ down bool }

// NewMoveUp returns move_statement_up.
func NewMoveUp() *MoveStatement { return &MoveStatement{} }

// NewMoveDown returns move_statement_down.
func NewMoveDown() *MoveStatement { return &MoveStatement{down: true} }

func (m *MoveStatement) Name() string {
	if m.down {
		return "move_statement_down"
	}
	return "move_statement_up"
}

func (m *MoveStatement) Description() string {
	which, example := "before", `with log("start") then const n = beds.length, moving const n = beds.length up puts it first`
	if m.down {
		which, example = "after", `with log("start") then const n = beds.length, moving log("start") down puts it second`
	}
	return "Swap a statement with the one " + which + " it in the same block, each with the comments directly above it: " + example + `. Use it to bring statements that belong together next to each other, e.g. before extract_function.
Identify the statement by file and the line it starts on (the outermost statement there, so a whole if or loop moves as one).
Behavior does not change, so it refuses, saying why, when the order matters: one uses what the other declares (a function declaration aside), or assigns; both may change state, throw or return; one changes state the other reads; a line with more than the statement on it.`
}

func (*MoveStatement) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + fileProp + `,
    "line": {"type": "integer", "description": "1-based line on which the statement to move starts."}
  },
  "required": ["file", "line"],
  "additionalProperties": false
}`)
}

func (m *MoveStatement) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File string `json:"file"`
		Line int    `json:"line"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	dir := "up"
	if m.down {
		dir = "down"
	}
	return plan(ws, a.File, fmt.Sprintf("moved the statement on line %d %s", a.Line, dir), func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		return p.MoveStatement(bg, path, a.Line, m.down)
	})
}

// InvertIf negates an if's condition and swaps its branches.
type InvertIf struct{}

// NewInvertIf returns invert_if.
func NewInvertIf() *InvertIf { return &InvertIf{} }

func (*InvertIf) Name() string { return "invert_if" }

func (*InvertIf) Description() string {
	return `Negate an if's condition and swap its branches, turning nesting into guard clauses: if (bed.wet) { plant(p) } else { return } becomes if (!bed.wet) { return } followed by plant(p); a last if (x > 0) { total += x } in a loop body becomes if (!(x > 0)) { continue } followed by total += x. keep_else only swaps the branches.
An if without else is un-nested this way when it ends a function's body (return) or a loop's (continue). The negation is simplified where that is exact (=== and !== swap, ! drops, < and >= swap for strings, De Morgan for && and ||; a number may be NaN, so x > 0 becomes !(x > 0)).
Identify the if by file and the line of its if keyword (column if the line has two).
Refuses, saying why: an else-if in the else branch, an if without else anywhere else, a name un-nesting would make clash.`
}

func (*InvertIf) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + fileProp + `,
    "line":      {"type": "integer", "description": "1-based line of the if keyword."},
    "column":    {"type": "integer", "description": "Optional 1-based column of the if keyword, only needed if the line has more than one if."},
    "keep_else": {"type": "boolean", "description": "Only swap the branches of an if/else; do not remove the else even if the new first branch exits."}
  },
  "required": ["file", "line"],
  "additionalProperties": false
}`)
}

func (*InvertIf) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File     string `json:"file"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
		KeepElse bool   `json:"keep_else"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return plan(ws, a.File, fmt.Sprintf("inverted the if on line %d", a.Line), func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		return p.InvertIf(bg, path, a.Line, a.Column, a.KeepElse)
	})
}

// RemoveElse un-nests an else after a branch that cannot fall through.
type RemoveElse struct{}

// NewRemoveElse returns remove_else.
func NewRemoveElse() *RemoveElse { return &RemoveElse{} }

func (*RemoveElse) Name() string { return "remove_else" }

func (*RemoveElse) Description() string {
	return `Remove an else after an if branch that cannot fall through, un-nesting it: if (!bed) { return 0 } else { return bed.depth * 2 } becomes if (!bed) { return 0 } followed by return bed.depth * 2. An else-if becomes a separate if. When it is the else branch that exits, use invert_if instead.
Identify the if by file and the line of its if keyword (column if the line has two).
Refuses, saying why: an if branch that can fall through, un-nested names that would clash.`
}

func (*RemoveElse) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + fileProp + `,
    "line":   {"type": "integer", "description": "1-based line of the if keyword."},
    "column": {"type": "integer", "description": "Optional 1-based column of the if keyword, only needed if the line has more than one if."}
  },
  "required": ["file", "line"],
  "additionalProperties": false
}`)
}

func (*RemoveElse) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File   string `json:"file"`
		Line   int    `json:"line"`
		Column int    `json:"column"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return plan(ws, a.File, fmt.Sprintf("removed the else of the if on line %d", a.Line), func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		return p.RemoveElse(bg, path, a.Line, a.Column)
	})
}

// ExtractFunction moves statements into a new function.
type ExtractFunction struct{}

// NewExtractFunction returns extract_function.
func NewExtractFunction() *ExtractFunction { return &ExtractFunction{} }

func (*ExtractFunction) Name() string { return "extract_function" }

func (*ExtractFunction) Description() string {
	return `Move a run of whole statements into a new function, declared after the one they are in, and call it in their place: in function report(beds: Bed[]), extracting let total = 0 and the loop that adds each b.depth to it, as sum, gives const total = sum(beds) there and function sum(beds: Bed[]) { let total = 0; for ...; return total; }. Locals the statements use become parameters; those they declare or assign that are used elsewhere come back as the result (an object of them when several). Statements that use this become a private method of the class; with await, the function is async and the call awaited.
Identify the statements by file, start_line and end_line: whole statements of one block, as inspect numbers them; or a case's body by function and case ("fern" in function water, or default; a last break stays), which stays valid after earlier steps. doc becomes the new function's doc comment.
Returns: when the lines end the function's body, their returns stay and the call becomes return sum(...).
Lines that return before the function's end return a value the call returns unless it is undefined, or whether they returned. Refuses, saying why: an early return of what may be undefined, a break or continue out of the lines, yield, arguments, this outside a class's method, a type parameter or a type declared in the function, a variable declared in a block of the lines and used after them, declaring some results and assigning others.`
}

func (*ExtractFunction) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + fileProp + `,
    "start_line": {"type": "integer", "description": "1-based first line of the statements to extract."},
    "end_line":   {"type": "integer", "description": "1-based last line of the statements to extract (inclusive)."},
    "function":   {"type": "string",  "description": "Instead of the lines: the function (name or Class.method) with the switch whose case to extract."},
    "case":       {"type": "string",  "description": "With function: the case's value as written (\"fern\", Kind.Fern) or default."},
    "name":       {"type": "string",  "description": "Name for the new function."},
    "doc":        {"type": "string",  "description": "Optional doc comment for the new function, its text without /** */."}
  },
  "required": ["file", "name"],
  "additionalProperties": false
}`)
}

func (*ExtractFunction) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File      string `json:"file"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
		Name      string `json:"name"`
		Doc       string `json:"doc"`
		Function  string `json:"function"`
		Case      string `json:"case"`
		Split     *bool  `json:"split"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	switch {
	case (a.Case != "") != (a.Function != ""):
		return nil, refactor.Refusef("give function and case together, for a case's body; or start_line and end_line")
	case a.Case != "" && (a.StartLine != 0 || a.EndLine != 0):
		return nil, refactor.Refusef("give either function and case or start_line and end_line, not both")
	case a.Case != "":
		path := ws.Abs(a.File)
		first, last, why := ws.CaseLines(path, a.Function, a.Case, false)
		if why != "" {
			return nil, refactor.Refusef("%s", why)
		}
		a.StartLine, a.EndLine = first, last
	}
	if a.StartLine == 0 || a.EndLine == 0 {
		return nil, refactor.Refusef("give start_line and end_line")
	}
	return plan(ws, a.File, fmt.Sprintf("extracted lines %d-%d as %s", a.StartLine, a.EndLine, a.Name), func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		return p.ExtractFunction(bg, path, a.StartLine, a.EndLine, a.Name, a.Doc)
	})
}

// InlineFunction replaces calls with the function's body.
type InlineFunction struct{}

// NewInlineFunction returns inline_function.
func NewInlineFunction() *InlineFunction { return &InlineFunction{} }

func (*InlineFunction) Name() string { return "inline_function" }

func (*InlineFunction) Description() string {
	return `Replace a call with the function's body, and delete the function when nothing else uses it: the inverse of extract_function. With function area(b: Bed) { return b.width * b.depth; }, const a = area(bed) becomes const a = bed.width * bed.depth. A body that is one expression or one return may be inlined anywhere, and so may guards and a last return (if (s.length <= n) return s; return s.slice(0, n); becomes s.length <= n ? s : s.slice(0, n)); a body of statements where the call is a statement, a declaration's or assignment's value, or a return, and one that returns early only where the call is a return's whole value. Parameters are replaced by the arguments where that is safe, and otherwise declared first (const b = arg;; a rest parameter as the array of the rest, a destructured one as its pattern). Of overloads, the implementation is inlined.
Identify the call by file, line and the function's name (column if the name is on the line twice). With all_calls, every call in the solution is inlined, other files' imports of it go, and the function is deleted (give file and name); if one call cannot be, none is. keep_function keeps the function; an exported one is always kept.
Refuses, saying why: methods a subclass could override (only static and private ones are inlined), async functions and generators, a body that names a type parameter, a destructured rest parameter, an early return elsewhere than a return's value, super, recursion, a name the body uses that means something else at the call.`
}

func (*InlineFunction) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":          {"type": "string",  "description": "Path of the file with the call, relative to the root."},
    "line":          {"type": "integer", "description": "1-based line of the call. With all_calls, optional: the line of any call or of the declaration."},
    "name":          {"type": "string",  "description": "Name of the called function or method (for a call x.m(), m)."},
    "column":        {"type": "integer", "description": "Optional 1-based column of the name, only needed if it appears more than once on the line."},
    "keep_function": {"type": "boolean", "description": "Keep the function even if no other call to it is left. Default false."},
    "all_calls":     {"type": "boolean", "description": "Inline every call to the function, in every file, then delete it (unless keep_function). Without line, name is a function (name or Class.method) of file."}
  },
  "required": ["file", "name"],
  "additionalProperties": false
}`)
}

func (*InlineFunction) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File   string `json:"file"`
		Line   int    `json:"line"`
		Name   string `json:"name"`
		Column int    `json:"column"`
		Keep   bool   `json:"keep_function"`
		All    bool   `json:"all_calls"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	deleted := false
	p, err := plan(ws, a.File, "", func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		es, del, why := p.InlineFunction(bg, path, a.Line, a.Name, a.Column, a.Keep, a.All)
		deleted = del
		return es, why
	})
	if err != nil {
		return nil, err
	}
	p.Summary = "inlined " + a.Name
	if a.All {
		p.Summary = "inlined every call of " + a.Name
	}
	if deleted {
		p.Summary += " and deleted it"
	}
	return p, nil
}
