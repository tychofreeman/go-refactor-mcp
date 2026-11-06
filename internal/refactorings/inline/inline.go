// Package inline implements inline, published in place of
// inline_function, inline_variable and inline_struct: it runs the one that
// what its arguments name asks for.
package inline

import (
	"encoding/json"
	"go/types"

	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/inlinefunc"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/inlinestruct"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/inlinevar"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// Inline is the "inline" refactoring.
type Inline struct{ refactor.Named[args] }

// New returns inline.
func New() *Inline { return &Inline{Named: refactor.Called[args]("inline")} }

func (*Inline) Description() string {
	return `Inline what name names into where it is used, and remove it when nothing else uses it; what it is says which:
- a function or method, at a call (line): with func full(u User) string { return u.First + " " + u.Last }, name := full(u) becomes name := u.First + " " + u.Last. all_calls inlines every call and deletes the function (line optional; keep_function keeps it).
- a local variable or constant, at its declaration or a use (line), or by function and name: name := u.First; return greet(name) becomes return greet(u.First).
- with field, a struct the struct name holds: with type Bed struct { Soil; name string } and type Soil struct { depth int }, name Bed and field Soil give type Bed struct { depth int; name string }, and b.Soil.depth becomes b.depth. elsewhere and prefer settle a struct used outside the holder and methods both have.
Refuses, saying why: what each of those refuses (a generic or variadic function, a variable assigned after its declaration, a value evaluated another number of times or in another order).`
}

// args are inline's: those of the tools it stands for.
type args struct {
	File         string `json:"file" desc:"Path of the file with the call, the variable, or the holder, relative to the module root."`
	Line         int    `json:"line,omitempty" desc:"1-based line of the call, or of the variable's declaration or a use."`
	Name         string `json:"name" desc:"The function or method called (for x.m(), m), the variable, or with field the holding struct."`
	Column       int    `json:"column,omitempty" desc:"Optional 1-based column of name, only needed if it occurs on the line more than once."`
	Function     string `json:"function,omitempty" desc:"Instead of line, for a variable: the function (Name or Type.Method) that declares it."`
	AllCalls     bool   `json:"all_calls,omitempty" desc:"For a function: every call in the module, then delete it."`
	KeepFunction bool   `json:"keep_function,omitempty" desc:"For a function: keep it even when no call is left."`
	Field        string `json:"field,omitempty" desc:"The field of name that holds the struct to inline: the embedded type's name, or the field's."`
	Elsewhere    string `json:"elsewhere,omitempty" desc:"With field, when the struct is used outside the holder: refuse (default), or keep."`
	Prefer       string `json:"prefer,omitempty" desc:"With field, when both have a method of one name: outer or inner."`
}

// Covers names the tools inline stands for.
func (*Inline) Covers() []string {
	return []string{"inline_function", "inline_variable", "inline_struct"}
}

// LocalsOnly leaves out the note on finding a declaration by name alone:
// inline needs a line, all_calls, function or field to know what to run.
func (*Inline) LocalsOnly() {}

// Plan runs the tool what args name asks for, with the arguments it takes.
func (*Inline) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	var to refactor.Refactoring
	switch {
	case a.Field != "":
		to = inlinestruct.New()
	case a.AllCalls || a.KeepFunction:
		to = inlinefunc.New()
	case a.Function != "":
		to = inlinevar.New()
	case a.Line != 0:
		pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
		if err != nil {
			return nil, err
		}
		switch obj := pkg.TypesInfo.ObjectOf(id).(type) {
		case *types.Func:
			to = inlinefunc.New()
		case *types.Var:
			if obj.IsField() {
				return nil, refactor.Refusef("%s on line %d is a field; to inline the struct a field holds, give name (the holder) and field", a.Name, a.Line)
			}
			to = inlinevar.New()
		case *types.Const:
			to = inlinevar.New()
		default:
			return nil, refactor.Refusef("%s on line %d is neither a function nor a local variable or constant; inline takes a call, a local, or a holder with field", a.Name, a.Line)
		}
	default:
		return nil, refactor.Refusef("give line (of a call or of a local's declaration or use), all_calls for every call of a function, function for a local by name, or field for the struct a struct holds")
	}
	return to.Plan(ws, refactor.OnlyArgs(raw, to.InputSchema()))
}
