package changesig

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

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
	return `Remove a parameter the body does not use, and its argument at every call in the workspace: removing retries, func send(to string, retries int) becomes func send(to string), and send(addr, 3) becomes send(addr). Use it last when taking a feature out: first change the body so it no longer uses the parameter. A body whose first mention of it assigns it (n, err := f()) does not use it.
Identify the function by file and name, or by line at its declaration or any call, and give the parameter. A dropped argument must have no side effects and not be able to panic.
Refuses, saying why: a body that reads the parameter, a function used as a value, a method that would start or stop implementing an interface from outside the module, an argument whose removal leaves a variable or import unused.`
}

func (*RemoveParameter) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":      {"type": "string",  "description": "Path of a file containing the function's declaration or a call, relative to the workspace root."},
    "line":      {"type": "integer", "description": "1-based line of that occurrence. Omit it to find name as a symbol of the package of file."},
    "name":      {"type": "string",  "description": "The function's name: with line, just the method name for a method; without line, the package-level name or Type.Method."},
    "column":    {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "parameter": {"type": "string",  "description": "Name of the parameter to remove."}
  },
  "required": ["file", "name", "parameter"],
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
	t, err := findFamily(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	at, err := t.named().paramIndex(a.Parameter)
	if err != nil {
		return nil, err
	}
	var cuts []cut
	for _, m := range t.group() {
		if err := m.checkUnused(m.params[at]); err != nil {
			return nil, err
		}
		if err := m.checkInterfaces(func(ps []string) []string { return slices.Delete(ps, at, at+1) }); err != nil {
			return nil, err
		}
		variadic := m.obj.Signature().Variadic() && at == len(m.params)-1
		cuts = append(cuts, m.paramCut(m.params[at]))
		for _, c := range m.calls {
			if err := checkNoSpread(c, m.WS.Position(c.expr.Pos())); err != nil {
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
				if bad, why := unsafeValue(funcAround(c), arg, m.WS.Effects()); bad != nil {
					return nil, refactor.Refusef("the argument %s at %s %s (%s), so not passing it would change behavior; move it out of the call first (for example `_ = %s`)",
						types.ExprString(arg), m.WS.Position(arg.Pos()), why, types.ExprString(bad.(ast.Expr)), types.ExprString(arg))
				}
			}
			cuts = append(cuts, m.argCut(c.expr, at, variadic))
		}
	}
	if err := t.checkNothingLeftUnused(cuts); err != nil {
		return nil, err
	}

	var fes []edit.FileEdit
	for _, c := range cuts {
		fes = append(fes, c.fileEdit())
	}
	for _, m := range t.group() {
		fes = append(fes, m.fixes...)
	}
	return t.newPlan(fmt.Sprintf("removed parameter %q from %s", a.Parameter, t.groupName()), fes), nil
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
	if t.decl == nil {
		return nil // an interface method has no body
	}
	info := t.Pkg.TypesInfo
	v := info.Defs[p.field.Names[p.index]]
	for id, o := range info.Uses {
		if v != nil && o == v {
			if fix, ok := t.deadOnEntry(v); ok {
				t.fixes = append(t.fixes, fix...)
				break
			}
			return refactor.Refusef("the body of %s uses %s (at %s); remove that use first", t.obj.Name(), p.name, t.WS.Position(id.Pos()))
		}
	}
	if t.obj.Signature().TypeParams().Len() > 0 && containsTypeParam(info.TypeOf(p.field.Type)) {
		return refactor.Refusef("the type of %s mentions a type parameter of %s, which calls may infer from its argument; this is not supported yet", p.name, t.obj.Name())
	}
	return nil
}

// deadOnEntry reports whether the body never reads the value v, a
// parameter, is called with: the first top-level statement that mentions v
// assigns it a value of its own type without reading it. Then v can go,
// and fix turns that statement's "=" into ":=" so that it declares v.
func (t *target) deadOnEntry(v types.Object) (fix []edit.FileEdit, ok bool) {
	info := t.Pkg.TypesInfo
	mentions := func(n ast.Node) bool {
		found := false
		ast.Inspect(n, func(m ast.Node) bool {
			if id, isIdent := m.(*ast.Ident); isIdent && info.Uses[id] == v {
				found = true
			}
			return !found
		})
		return found
	}
	for _, st := range t.decl.Body.List {
		if !mentions(st) {
			continue
		}
		as, isAssign := st.(*ast.AssignStmt)
		if !isAssign || (as.Tok != token.DEFINE && as.Tok != token.ASSIGN) {
			return nil, false
		}
		for _, r := range as.Rhs {
			if mentions(r) {
				return nil, false
			}
		}
		at := -1
		for i, l := range as.Lhs {
			if id, isIdent := l.(*ast.Ident); isIdent && info.Uses[id] == v {
				at = i
			}
		}
		if at < 0 {
			return nil, false // v.f = ... or the like, which reads v
		}
		var typ types.Type
		if len(as.Rhs) == len(as.Lhs) {
			typ = info.TypeOf(as.Rhs[at])
		} else if tuple, isTuple := info.TypeOf(as.Rhs[0]).(*types.Tuple); isTuple && at < tuple.Len() {
			typ = tuple.At(at).Type()
		}
		if typ == nil || !types.Identical(typ, v.Type()) {
			return nil, false // a v declared there would have another type
		}
		if as.Tok == token.DEFINE {
			return nil, true
		}
		if len(as.Lhs) != 1 {
			return nil, false // ":=" would declare the others anew too
		}
		path := t.WS.Fset.Position(as.TokPos).Filename
		return []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: t.Off(as.TokPos), End: t.Off(as.TokPos) + 1, NewText: ":="}}}}, true
	}
	return nil, false
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
	list := t.ftype.Params.List
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
	return cut{path: t.WS.Fset.Position(from).Filename, start: t.Off(from), end: t.Off(to)}
}

func (c cut) fileEdit() edit.FileEdit {
	return edit.FileEdit{Path: c.path, Edits: []edit.TextEdit{{Start: c.start, End: c.end}}}
}

// checkNothingLeftUnused refuses cuts that delete every use of a local
// variable or of an import, which would no longer compile.
func (t *target) checkNothingLeftUnused(cuts []cut) error {
	inCut := func(pos token.Pos) bool {
		p := t.WS.Fset.Position(pos)
		for _, c := range cuts {
			if p.Filename == c.path && c.start <= p.Offset && p.Offset < c.end {
				return true
			}
		}
		return false
	}
	for _, p := range t.WS.Pkgs {
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
			where := t.WS.Position(id.Pos())
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
