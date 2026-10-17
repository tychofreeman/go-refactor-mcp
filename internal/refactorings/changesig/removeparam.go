package changesig

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// RemoveParameter is the "remove_parameter" refactoring.
type RemoveParameter struct{}

// NewRemoveParameter returns the remove_parameter refactoring.
func NewRemoveParameter() *RemoveParameter { return &RemoveParameter{} }

func (*RemoveParameter) Name() string { return "remove_parameter" }

func (*RemoveParameter) Description() string {
	return `Remove a parameter the body does not use from a function or unexported method, and drop its argument from every call in the workspace.
Use it to finish removing a feature: first change the body so it no longer uses the parameter, then remove it.
Identify the function by file, line and name, at its declaration or any call, and give the parameter's name. Removing a variadic parameter drops all of its arguments.
Each dropped argument must have no side effects and must not be able to panic (no calls, pointer dereferences, indexing or non-constant division), so that not evaluating it changes nothing.
Refuses if the body uses the parameter, if the function is used other than by calling it, if it is an exported or interface method, if a dropped argument would leave a local variable or an import unused, or if the parameter's type is all that lets calls infer a type parameter.`
}

func (*RemoveParameter) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":      {"type": "string",  "description": "Path of a file containing the function's declaration or a call, relative to the workspace root."},
    "line":      {"type": "integer", "description": "1-based line of that occurrence."},
    "name":      {"type": "string",  "description": "The function's name (for a method, just the method name)."},
    "column":    {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "parameter": {"type": "string",  "description": "Name of the parameter to remove."}
  },
  "required": ["file", "line", "name", "parameter"],
  "additionalProperties": false
}`)
}

type removeArgs struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Column    int    `json:"column,omitempty"`
	Name      string `json:"name"`
	Parameter string `json:"parameter"`
}

// cut is a range of source text the refactoring deletes.
type cut struct {
	path       string
	start, end int
}

// Plan checks preconditions and computes the edits.
func (*RemoveParameter) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a removeArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	t, err := findTarget(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	at, err := t.paramIndex(a.Parameter)
	if err != nil {
		return nil, err
	}
	if err := t.checkUnused(t.params[at]); err != nil {
		return nil, err
	}
	variadic := t.obj.Signature().Variadic() && at == len(t.params)-1

	cuts := []cut{t.paramCut(t.params[at])}
	for _, c := range t.calls {
		if err := checkNoSpread(c, t.ws.Position(c.expr.Pos())); err != nil {
			return nil, err
		}
		args := c.expr.Args
		if at >= len(args) {
			continue // a variadic parameter given no arguments
		}
		dropped := args[at : at+1]
		if variadic {
			dropped = args[at:]
		}
		for _, arg := range dropped {
			if bad, why := unsafeValue(c.pkg.TypesInfo, arg); bad != nil {
				return nil, refactor.Refusef("the argument %s at %s %s (%s), so not passing it would change behavior; move it out of the call first (for example `_ = %s`)",
					types.ExprString(arg), t.ws.Position(arg.Pos()), why, types.ExprString(bad.(ast.Expr)), types.ExprString(arg))
			}
		}
		cuts = append(cuts, t.argCut(c.expr, at, variadic))
	}
	if err := t.checkNothingLeftUnused(cuts); err != nil {
		return nil, err
	}

	var fes []edit.FileEdit
	for _, c := range cuts {
		fes = append(fes, c.fileEdit())
	}
	return t.newPlan(fmt.Sprintf("removed parameter %q from %s", a.Parameter, t.obj.Name()), fes), nil
}

// paramIndex returns the index of the named parameter in the flattened
// parameter list.
func (t *target) paramIndex(name string) (int, error) {
	at := -1
	var names []string
	for i, p := range t.params {
		names = append(names, p.name)
		if p.name != name {
			continue
		}
		if at >= 0 {
			return 0, refactor.Refusef("%s has more than one parameter named %q", t.obj.Name(), name)
		}
		at = i
	}
	if at < 0 {
		return 0, refactor.Refusef("%s has no parameter %q; its parameters are %v", t.obj.Name(), name, names)
	}
	return at, nil
}

// checkUnused refuses a parameter the body uses, or whose type is needed
// to infer a type parameter at calls.
func (t *target) checkUnused(p param) error {
	info := t.pkg.TypesInfo
	v := info.Defs[p.field.Names[p.index]]
	for id, o := range info.Uses {
		if v != nil && o == v {
			return refactor.Refusef("the body of %s uses %s (at %s); remove that use first", t.obj.Name(), p.name, t.ws.Position(id.Pos()))
		}
	}
	if t.obj.Signature().TypeParams().Len() > 0 && containsTypeParam(info.TypeOf(p.field.Type)) {
		return refactor.Refusef("the type of %s mentions a type parameter of %s, which calls may infer from its argument; this is not supported yet", p.name, t.obj.Name())
	}
	return nil
}

// paramCut deletes the parameter's name, or its whole field when it is the
// field's only name, with one adjacent comma.
func (t *target) paramCut(p param) cut {
	if names := p.field.Names; len(names) > 1 {
		if p.index < len(names)-1 {
			return t.cut(names[p.index].Pos(), names[p.index+1].Pos())
		}
		return t.cut(names[p.index-1].End(), names[p.index].End())
	}
	list := t.decl.Type.Params.List
	j := 0
	for list[j] != p.field {
		j++
	}
	switch {
	case j < len(list)-1:
		return t.cut(list[j].Pos(), list[j+1].Pos())
	case j > 0:
		return t.cut(list[j-1].End(), list[j].End())
	}
	return t.cut(list[j].Pos(), list[j].End())
}

// argCut deletes the call's argument at index at, or, for a variadic
// parameter, every argument from at on (and a trailing "...").
func (t *target) argCut(ce *ast.CallExpr, at int, variadic bool) cut {
	args := ce.Args
	last := at
	end := args[at].End()
	if variadic {
		last = len(args) - 1
		end = args[last].End()
		if ce.Ellipsis.IsValid() {
			end = ce.Ellipsis + token.Pos(len("..."))
		}
	}
	switch {
	case last < len(args)-1:
		return t.cut(args[at].Pos(), args[last+1].Pos())
	case at > 0:
		return t.cut(args[at-1].End(), end)
	}
	return t.cut(args[at].Pos(), end)
}

func (t *target) cut(from, to token.Pos) cut {
	return cut{path: t.ws.Fset.Position(from).Filename, start: t.off(from), end: t.off(to)}
}

func (c cut) fileEdit() edit.FileEdit {
	return edit.FileEdit{Path: c.path, Edits: []edit.TextEdit{{Start: c.start, End: c.end}}}
}

// checkNothingLeftUnused refuses cuts that delete every use of a local
// variable or of an import, which would no longer compile.
func (t *target) checkNothingLeftUnused(cuts []cut) error {
	inCut := func(pos token.Pos) bool {
		p := t.ws.Fset.Position(pos)
		for _, c := range cuts {
			if p.Filename == c.path && c.start <= p.Offset && p.Offset < c.end {
				return true
			}
		}
		return false
	}
	for _, p := range t.ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		params := paramVars(p)
		cutUse := map[types.Object]*ast.Ident{}
		kept := map[types.Object]bool{}
		for id, o := range p.TypesInfo.Uses {
			if !mustBeUsed(o, params) {
				continue
			}
			if inCut(id.Pos()) {
				cutUse[o] = id
			} else {
				kept[o] = true
			}
		}
		for o, id := range cutUse {
			if kept[o] {
				continue
			}
			where := t.ws.Position(id.Pos())
			if pn, ok := o.(*types.PkgName); ok {
				return refactor.Refusef("the code removed at %s is the only use of the import %q in its file, which would then be unused; this is not supported yet",
					where, pn.Imported().Path())
			}
			return refactor.Refusef("the argument at %s is the only use of the local variable %s, which would then be unused; remove the variable first or use it elsewhere",
				where, o.Name())
		}
	}
	return nil
}

// mustBeUsed reports whether o is something the compiler requires to be
// used: an import, or a local variable other than a parameter or result.
func mustBeUsed(o types.Object, params map[types.Object]bool) bool {
	switch o := o.(type) {
	case *types.PkgName:
		return true
	case *types.Var:
		return o.Parent() != nil && o.Parent() != o.Pkg().Scope() && !params[o]
	}
	return false
}

// paramVars returns the receivers, parameters and results of every
// function in p, which the compiler does not require to be used.
func paramVars(p *packages.Package) map[types.Object]bool {
	vars := map[types.Object]bool{}
	for _, f := range p.Syntax {
		ast.Inspect(f, func(n ast.Node) bool {
			var lists []*ast.FieldList
			switch n := n.(type) {
			case *ast.FuncDecl:
				lists = append(lists, n.Recv)
			case *ast.FuncType:
				lists = append(lists, n.Params, n.Results)
			}
			for _, l := range lists {
				if l == nil {
					continue
				}
				for _, fld := range l.List {
					for _, name := range fld.Names {
						vars[p.TypesInfo.Defs[name]] = true
					}
				}
			}
			return true
		})
	}
	return vars
}
