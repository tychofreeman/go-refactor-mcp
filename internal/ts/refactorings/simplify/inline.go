package simplify

import (
	"encoding/json"
	"fmt"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// Inline is inline for TypeScript, published in place of inline_function
// and inline_variable: it runs the one that what name is asks for.
type Inline struct{ refactor.Named[inlineArgs] }

// NewInline returns inline.
func NewInline() *Inline { return &Inline{Named: refactor.Called[inlineArgs]("inline")} }

func (*Inline) Description() string {
	return `Inline what name names into where it is used, and remove it when nothing else uses it; what it is says which:
- a function or method, at a call (line): with function area(b: Bed) { return b.width * b.depth; }, const a = area(bed) becomes const a = bed.width * bed.depth. all_calls inlines every call in every file and deletes the function (line optional; keep_function keeps it; an exported one is kept).
- a local variable or constant, at its declaration or a use (line), or by function and name: const tall = p.height * 2; return tall + margin; becomes return p.height * 2 + margin;
- with field, a class the class name holds: with class Soil { depth = 3 } and class Bed { soil = new Soil() }, name Bed and field soil move depth into Bed, b.soil.depth becomes b.depth, and Soil goes.`
}

// inlineArgs are inline's: those of the tools it stands for.
type inlineArgs struct {
	File     string `json:"file" desc:"Path of the file with the call or the variable, relative to the root."`
	Line     int    `json:"line,omitempty" desc:"1-based line of the call, or of the variable's declaration or a use."`
	Name     string `json:"name" desc:"The function or method called (for x.m(), m), the variable, or with field the holding class."`
	Column   int    `json:"column,omitempty" desc:"Optional 1-based column of name, only needed if it occurs on the line more than once."`
	Function string `json:"function,omitempty" desc:"Instead of line, for a variable: the function (name or Class.method) that declares it."`
	Keep     bool   `json:"keep_function,omitempty" desc:"For a function: keep it even when no call is left."`
	All      bool   `json:"all_calls,omitempty" desc:"For a function: every call, in every file, then delete it."`
	Field    string `json:"field,omitempty" desc:"The property of name (a class) that holds the class to inline."`
}

// Covers names the tools inline stands for.
func (*Inline) Covers() []string {
	return []string{"inline_function", "inline_variable", "inline_struct"}
}

// Plan runs the tool what args name asks for, with the arguments it takes.
func (*Inline) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a inlineArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	var to refactor.RefactoringOf[*workspace.Workspace]
	switch {
	case a.Field != "":
		to = NewInlineStruct()
	case a.All || a.Keep:
		to = NewInlineFunction()
	case a.Function != "":
		to = NewInlineVariable()
	case a.Line != 0:
		path, p, why := ws.Project(a.File)
		if why != "" {
			return nil, refactor.Refusef("%s", why)
		}
		callable, found := p.Callable(bg, path, a.Line, a.Name, a.Column)
		if !found {
			return nil, refactor.Refusef("no %s on line %d of %s", a.Name, a.Line, a.File)
		}
		to = NewInlineVariable()
		if callable {
			to = NewInlineFunction()
		}
	default:
		return nil, refactor.Refusef("give line (of a call or of a local's declaration or use), all_calls for every call of a function, or function for a local by name")
	}
	return to.Plan(ws, refactor.OnlyArgs(raw, to.InputSchema()))
}

// InlineStruct is TypeScript's inline_struct: a class held in a property
// inlined into the class that holds it, as Go's inline_struct does for a
// struct. inline runs it for field.
type InlineStruct struct {
	refactor.Named[inlineStructArgs]
}

// NewInlineStruct returns inline_struct.
func NewInlineStruct() *InlineStruct {
	return &InlineStruct{Named: refactor.Called[inlineStructArgs]("inline_struct")}
}

func (*InlineStruct) Description() string {
	return "Inline a class held in a property into the class that holds it: with class Soil { depth = 3 } and class Bed { soil = new Soil() }, name Bed and field soil move depth into Bed, b.soil.depth becomes b.depth, and Soil goes. The property must be initialized with new S() with no arguments, S be a class of the same file used nowhere else, with no constructor, static members or heritage, and the property used only as x.soil.m."
}

// inlineStructArgs are the arguments of inline_struct.
type inlineStructArgs struct {
	File  string `json:"file" desc:"Path of the file with the holding class, relative to the root."`
	Name  string `json:"name" desc:"The holding class."`
	Field string `json:"field" desc:"The property that holds the class to inline."`
}

func (*InlineStruct) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a inlineStructArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return plan(ws, a.File, fmt.Sprintf("inlined the class %s.%s holds into %s", a.Name, a.Field, a.Name), func(p *tsapi.Project, path string) ([]tsapi.Edit, string) {
		return p.InlineClass(bg, path, a.Name, a.Field)
	})
}
