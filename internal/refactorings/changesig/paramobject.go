package changesig

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/adddecl"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/fold"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/inlinefunc"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/rename"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// ParamsToObject is the "convert_params_to_object" refactoring for Go
// (Fowler's Introduce Parameter Object).
type ParamsToObject struct{ refactor.Named[objectArgs] }

// NewParamsToObject returns convert_params_to_object.
func NewParamsToObject() *ParamsToObject {
	return &ParamsToObject{Named: refactor.Called[objectArgs]("convert_params_to_object")}
}

func (*ParamsToObject) Description() string {
	return `Bundle a function's parameters into one struct that every call builds: with func water(bed string, litres int) string, water("roses", 3) becomes water(waterArgs{bed: "roses", litres: 3}), and the body reads a.bed and a.litres (a variable of the same name where it assigns one). type_name names the struct, declared just before the function (by default named after the function, exported with it); its fields are the parameters, exported when a call is in another package.
Identify the function by file and name (Type.Method for a method).
It is four steps, one change: declare the struct and a function taking it, fold the calls into that, inline the old function there, and rename it back.`
}

// objectArgs are the arguments of convert_params_to_object.
type objectArgs struct {
	File     string `json:"file" desc:"Path of the file declaring the function, relative to the workspace root."`
	Name     string `json:"name" desc:"The function: Name, or Type.Method for a method."`
	TypeName string `json:"type_name,omitempty" desc:"Optional: the struct's name."`
}

// Plan is not reached: the refactoring runs as the steps Expand returns.
func (*ParamsToObject) Plan(*workspace.Workspace, json.RawMessage) (*refactor.Plan, error) {
	return nil, refactor.Refusef("convert_params_to_object runs as steps")
}

// Expand returns the four steps: add_declaration of the struct and of a
// function taking it that calls the old one with its fields, fold of that
// function (every call builds the struct), inline_function of the old one
// into it, and rename of it to the old name.
func (*ParamsToObject) Expand(ws *workspace.Workspace, raw json.RawMessage) ([]refactor.Step, error) {
	var a objectArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	st, err := site.At(ws, ws.Abs(a.File))
	if err != nil {
		return nil, err
	}
	recvType, name, _ := strings.Cut(a.Name, ".")
	if name == "" {
		recvType, name = "", recvType
	}
	decl := findDecl(st.File, recvType, name)
	if decl == nil {
		return nil, refactor.Refusef("%s declares no function %s", a.File, a.Name)
	}
	fn, _ := st.Info.Defs[decl.Name].(*types.Func)
	sig := fn.Type().(*types.Signature)
	params := sig.Params()
	switch {
	case decl.Type.TypeParams != nil || sig.RecvTypeParams() != nil:
		return nil, refactor.Refusef("%s is generic, which convert_params_to_object does not support yet", a.Name)
	case params.Len() == 0:
		return nil, refactor.Refusef("%s has no parameters", a.Name)
	case sig.Variadic():
		return nil, refactor.Refusef("%s is variadic; a struct field cannot take its arguments", a.Name)
	}
	recvName := ""
	if decl.Recv != nil {
		if len(decl.Recv.List[0].Names) == 0 || decl.Recv.List[0].Names[0].Name == "_" {
			return nil, refactor.Refusef("%s's receiver has no name; name it first", a.Name)
		}
		recvName = decl.Recv.List[0].Names[0].Name
	}
	export, err := checkCalls(ws, fn, decl)
	if err != nil {
		return nil, err
	}
	exported := ast.IsExported(name)
	typeName := a.TypeName
	if typeName == "" {
		typeName = name + "Args"
		if exported {
			typeName = strings.ToUpper(typeName[:1]) + typeName[1:]
		}
	}
	if !token.IsIdentifier(typeName) || st.Pkg.Types.Scope().Lookup(typeName) != nil || types.Universe.Lookup(typeName) != nil {
		return nil, refactor.Refusef("%s is already declared, or not a name; give type_name another", typeName)
	}
	used := map[string]bool{}
	ast.Inspect(decl, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			used[id.Name] = true
		}
		return true
	})
	var fields, passed []string
	seen := map[string]bool{}
	for i := range params.Len() {
		p := params.At(i)
		if p.Name() == "" || p.Name() == "_" {
			return nil, refactor.Refusef("parameter %d of %s has no name; name it first", i+1, a.Name)
		}
		f := p.Name()
		if export {
			f = strings.ToUpper(f[:1]) + f[1:]
		}
		if seen[f] {
			return nil, refactor.Refusef("two parameters of %s would both be field %s; rename one first", a.Name, f)
		}
		seen[f] = true
		typ, missing := typeIn(st, p.Type())
		if missing != "" {
			return nil, refactor.Refusef("the struct needs package %s, which %s does not import", missing, a.File)
		}
		fields = append(fields, "\t"+f+" "+typ)
		passed = append(passed, "args."+f)
	}
	if used["args"] {
		return nil, refactor.Refusef("%s already uses the name args, which the struct's parameter would take; rename it first", a.Name)
	}
	tmp := freshName(st, used, name+"Tmp")
	results := ""
	if decl.Type.Results != nil {
		results = " " + st.Text(decl.Type.Results)
	}
	call := name + "(" + strings.Join(passed, ", ") + ")"
	recv, qualified, tmpName := "", name, tmp
	if recvName != "" {
		recv = st.Text(decl.Recv) + " "
		call = recvName + "." + call
		qualified, tmpName = recvType+"."+name, recvType+"."+tmp
	}
	if results != "" {
		call = "return " + call
	}
	doc := "// " + tmp + " calls " + name + ".\n"
	if decl.Doc != nil {
		doc = st.Text(decl.Doc) + "\n"
	}
	content := fmt.Sprintf("// %s holds the arguments of %s.\ntype %s struct {\n%s\n}\n\n%sfunc %s%s(args %s)%s {\n\t%s\n}", typeName, name, typeName, strings.Join(fields, "\n"), doc, recv, tmp, typeName, results, call)
	step := func(r refactor.Refactoring, args map[string]any) refactor.Step {
		j, _ := json.Marshal(args)
		return refactor.Step{Refactoring: r, Args: j}
	}
	return []refactor.Step{
		step(adddecl.New(), map[string]any{"file": a.File, "before": qualified, "content": content}),
		step(fold.New(), map[string]any{"file": a.File, "name": tmpName}),
		step(inlinefunc.New(), map[string]any{"file": a.File, "name": qualified, "all_calls": true}),
		step(rename.New(), map[string]any{"file": a.File, "function": tmpName, "name": tmp, "new_name": name}),
	}, nil
}

// findDecl finds the function or method name of file; recvType names the
// method's receiver type, or is "" for a function.
func findDecl(file *ast.File, recvType, name string) *ast.FuncDecl {
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != name || fd.Body == nil {
			continue
		}
		if fd.Recv == nil && recvType == "" {
			return fd
		}
		if fd.Recv != nil && recvType != "" && receiverTypeName(fd.Recv.List[0].Type) == recvType {
			return fd
		}
	}
	return nil
}

// receiverTypeName is the name of the type in a receiver's type: T or *T.
func receiverTypeName(e ast.Expr) string {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// checkCalls refuses fn when a use of it is not a call, is a call in its
// own body, or passes one call of several results; export reports whether
// a call is in another package (an external test's included), where the
// struct's fields must be exported.
func checkCalls(ws *workspace.Workspace, fn *types.Func, decl *ast.FuncDecl) (export bool, err error) {
	at := ws.Fset.Position(fn.Pos())
	for _, p := range ws.Pkgs {
		for _, f := range p.Syntax {
			var stack []ast.Node
			ast.Inspect(f, func(n ast.Node) bool {
				if err != nil {
					return false
				}
				if n == nil {
					stack = stack[:len(stack)-1]
					return false
				}
				defer func() { stack = append(stack, n) }()
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				o := p.TypesInfo.Uses[id]
				if o == nil || ws.Fset.Position(o.Pos()) != at || o.Name() != fn.Name() {
					return true
				}
				pos := ws.Fset.Position(id.Pos())
				if id.Pos() > decl.Pos() && id.Pos() < decl.End() && pos.Filename == at.Filename {
					err = refactor.Refusef("%s calls itself, and inlining it would not end", fn.Name())
					return false
				}
				var fun ast.Node = id
				parent := stack[len(stack)-1]
				if sel, ok := parent.(*ast.SelectorExpr); ok && sel.Sel == id {
					fun, parent = sel, stack[len(stack)-2]
				}
				call, ok := parent.(*ast.CallExpr)
				if !ok || call.Fun != fun {
					err = refactor.Refusef("%s is used as a value at %s, which a call building the struct cannot replace", fn.Name(), ws.Position(id.Pos()))
					return false
				}
				if len(call.Args) != fn.Type().(*types.Signature).Params().Len() {
					err = refactor.Refusef("the call at %s passes one call of several results; assign them to variables first", ws.Position(call.Pos()))
					return false
				}
				if p.Types.Path() != fn.Pkg().Path() {
					export = true
				}
				return true
			})
		}
	}
	return export, err
}

// freshName returns base, or base with a number, that neither the
// function nor its package uses.
func freshName(st *site.Site, used map[string]bool, base string) string {
	name := base
	for i := 2; used[name] || st.Pkg.Types.Scope().Lookup(name) != nil; i++ {
		name = fmt.Sprintf("%s%d", base, i)
	}
	return name
}

// typeIn spells t as st's file must, or names the package it would need.
func typeIn(st *site.Site, t types.Type) (string, string) {
	imports := map[string]*types.Package{}
	for path, p := range st.Pkg.Imports {
		imports[path] = p.Types
	}
	return analysis.TypeString(t, st.File, st.Pkg.Types, imports)
}
