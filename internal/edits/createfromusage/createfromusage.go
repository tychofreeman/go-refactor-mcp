// Package createfromusage implements create_from_usage, a fix: it
// declares the functions code calls before they exist.
package createfromusage

import (
	"cmp"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// CreateFromUsage is the "create_from_usage" fix.
type CreateFromUsage struct{}

// New returns the create_from_usage fix.
func New() *CreateFromUsage { return &CreateFromUsage{} }

func (*CreateFromUsage) Name() string { return "create_from_usage" }

func (*CreateFromUsage) Description() string {
	return `Declare the functions code calls before they exist, as stubs ("create from usage"). This is an edit, not a refactoring, and the one step that may start from code that does not type-check: it must leave fewer errors and no new one.
Use it after writing a test or code that calls functions not written yet: give the file with the calls. Every function it calls that is not declared gets a stub, or only those in names; a call pkg.F declares F in that package of the module, and a call x.M, where x is (or points to) a type of the module without a field or method M, declares the method M on that type, with a receiver like its other methods'. On a generic type, the method is written with its type parameters: called on a Stack[string], a string argument becomes a T parameter. If that type is an interface, M is added to it, with a stub on each type of the module that implemented it, so that they still do. A type of the module used as an interface it lacks methods of (assigned, passed or returned as one, e.g. var _ io.Writer = (*T)(nil)) gets stubs of those methods, with the interface's signatures.
Each parameter has its argument's type and is named after it (a variable or field) or its type. The results come from how the value is used: a call as a statement has none, a returned call has the function's results, err is an error and ok a bool, and a variable has the type it is compared with, passed as, assigned to or returned as. When the use does not tell, give them in results. The body is panic("not implemented").
A stub goes in to_file; else a method goes in the file of its type, and a function in the calling file, if it is of that package and not a test; else in the file the test is named after (m.go for m_test.go), or the package's first file. The result's "chose:" lines give each signature and file: check them.`
}

func (*CreateFromUsage) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":    {"type": "string", "description": "Path of the file with the calls, relative to the workspace root."},
    "names":   {"type": "array", "items": {"type": "string"}, "description": "Optional: declare only these functions (by name, without package), not every undeclared one the file calls."},
    "results": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Optional: result lists by function name, as Go source (e.g. {\"Load\": \"(*Config, error)\"}), for uses that do not tell them."},
    "to_file": {"type": "string", "description": "Optional: the file of the target package to put the stubs in."}
  },
  "required": ["file"],
  "additionalProperties": false
}`)
}

func (*CreateFromUsage) ChangesBehavior() {}

func (*CreateFromUsage) Fixes() {}

type args struct {
	File    string            `json:"file"`
	Names   []string          `json:"names,omitempty"`
	Results map[string]string `json:"results,omitempty"`
	ToFile  string            `json:"to_file,omitempty"`
}

// Plan checks preconditions and computes the edits.
func (*CreateFromUsage) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	pkg, file := ws.FileSyntax(path)
	if file == nil {
		return nil, refactor.Refusef("file %s is not part of the loaded packages", a.File)
	}
	stubs := append(undeclared(ws, pkg, file), unimplemented(ws, pkg, file)...)
	if len(a.Names) > 0 {
		var chosen []stub
		for _, n := range a.Names {
			i := slices.IndexFunc(stubs, func(s stub) bool { return s.name == n })
			if i < 0 {
				return nil, refactor.Refusef("%s calls no undeclared function %s", a.File, n)
			}
			chosen = append(chosen, stubs[i])
		}
		stubs = chosen
	}
	if len(stubs) == 0 {
		return nil, refactor.Refusef("%s calls no function that is not declared, and uses no type as an interface it lacks methods of", a.File)
	}

	st := &stubber{ws: ws, pkg: pkg, file: file, caller: path, args: a}
	var decls []decl
	var names, choices []string
	for _, s := range stubs {
		ds, err := st.declare(s)
		if err != nil {
			return nil, err
		}
		decls = append(decls, ds...)
		names = append(names, s.fullName())
	}
	for _, d := range decls {
		if d.choice != "" {
			choices = append(choices, d.choice)
		}
	}
	edits, err := writeStubs(ws, decls)
	if err != nil {
		return nil, err
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("declared %s, as stubs", strings.Join(names, ", ")),
		Edits:   edits,
		Choices: choices,
	}, nil
}

// writeStubs writes decls into their files, importing what they use.
func writeStubs(ws *workspace.Workspace, decls []decl) ([]edit.FileEdit, error) {
	var order []string
	byFile := map[string][]decl{}
	for _, d := range decls {
		if byFile[d.file] == nil {
			order = append(order, d.file)
		}
		byFile[d.file] = append(byFile[d.file], d)
	}
	var edits []edit.FileEdit
	for _, dest := range order {
		dpkg, dfile := ws.FileSyntax(dest)
		src, err := os.ReadFile(dest)
		if err != nil {
			return nil, err
		}
		var tes []edit.TextEdit
		end, all := "", "" // all is what the file gets, as declarations: to find the packages it names
		for _, d := range byFile[dest] {
			if d.at < 0 {
				end += d.text
				all += d.text
				continue
			}
			tes = append(tes, edit.TextEdit{Start: d.at, End: d.at, NewText: d.text})
			all += "\ntype _ interface {\n" + d.text + "}\n"
		}
		if end != "" {
			tes = append(tes, edit.TextEdit{Start: len(src), End: len(src), NewText: end})
		}
		code, _, err := gocode.ParseDeclarations(all)
		if err != nil {
			return nil, err
		}
		adder := imports.New(ws, dpkg, dfile, src)
		if _, err := gocode.AddImports(ws, dpkg, dfile, adder, dfile.Name.Pos(), code, nil); err != nil {
			return nil, err
		}
		edits = append(edits, edit.FileEdit{Path: dest, Edits: append(tes, adder.Edits()...)})
	}
	return edits, nil
}

// decl is a declaration to write: text at offset at of file, or at its
// end if at is -1 (an offset is only for a method added to an interface),
// and the choice it is, for the result ("" for a line break that only
// spreads a one-line interface over lines).
type decl struct {
	file   string
	at     int
	text   string
	choice string
}

// stubber declares the stubs for the calls of one file.
type stubber struct {
	ws     *workspace.Workspace
	pkg    *packages.Package // of the file with the calls
	file   *ast.File
	caller string // its path
	args   args
}

// declare returns the declarations s needs.
func (st *stubber) declare(s stub) ([]decl, error) {
	switch {
	case s.method != nil:
		return st.implement(s)
	case s.recv != nil && types.IsInterface(s.recv):
		return st.extend(s)
	}
	home, recv, used := "", "", map[string]bool{}
	if s.recv != nil {
		home = st.ws.Fset.Position(s.recv.Obj().Pos()).Filename
		recv = receiver(st.pkg.TypesInfo, s.recv, s.x)
		used[strings.Fields(recv)[0]] = true
	}
	dest, err := destination(st.ws, s.target, st.caller, st.args.ToFile, home)
	if err != nil {
		return nil, err
	}
	sig, err := st.signature(s, writer(s.target.PkgPath, s.recv), used)
	if err != nil {
		return nil, err
	}
	if recv != "" {
		sig = "(" + recv + ") " + sig
	}
	return []decl{funcDecl(st.ws, dest, sig)}, nil
}

// signature is that of s's function, from its call: "Name(params)
// results".
func (st *stubber) signature(s stub, write func(types.Type) string, used map[string]bool) (string, error) {
	line := st.ws.Fset.Position(s.call.Pos()).Line
	params, err := parameters(st.pkg.TypesInfo, s.call, write, used)
	if err != nil {
		return "", refactor.Refusef("for %s, called on line %d of %s: %v", s.name, line, st.args.File, err)
	}
	res, ok := st.args.Results[s.name]
	if ok {
		res = resultText(res)
	} else if res, ok = results(st.pkg.TypesInfo, st.file, s.call, write); !ok {
		return "", refactor.Refusef("cannot tell the results of %s from its use on line %d of %s; give them in results, e.g. {%q: \"(int, error)\"}", s.name, line, st.args.File, s.name)
	}
	return s.name + "(" + strings.Join(params, ", ") + ")" + res, nil
}

// implement declares s.method, of an interface, on s.recv, with the
// interface's signature.
func (st *stubber) implement(s stub) ([]decl, error) {
	home := st.ws.Fset.Position(s.recv.Obj().Pos()).Filename
	dest, err := destination(st.ws, s.target, st.caller, st.args.ToFile, home)
	if err != nil {
		return nil, err
	}
	sig := sigString(s.method.Type().(*types.Signature), writer(s.target.PkgPath, s.recv))
	recv := receiver(st.pkg.TypesInfo, s.recv, s.x)
	return []decl{funcDecl(st.ws, dest, "("+recv+") "+s.name+sig)}, nil
}

// extend adds the method s calls to the interface s.recv, and a stub of it
// to each type of the module that implemented the interface, so that it
// still does.
func (st *stubber) extend(s stub) ([]decl, error) {
	obj := s.recv.Obj()
	pos := st.ws.Fset.Position(obj.Pos())
	_, f := st.ws.FileSyntax(pos.Filename)
	it := interfaceType(st.ws, f, pos.Offset)
	if it == nil {
		return nil, refactor.Refusef("cannot find the declaration of interface %s", obj.Name())
	}
	tf := st.ws.Fset.File(f.Pos())
	src, err := os.ReadFile(pos.Filename)
	if err != nil {
		return nil, err
	}
	sig, err := st.signature(s, writer(obj.Pkg().Path(), s.recv), map[string]bool{})
	if err != nil {
		return nil, err
	}
	choice := fmt.Sprintf("%s, in interface %s in %s", sig, obj.Name(), st.ws.Rel(pos.Filename))
	var decls []decl
	if tf.Line(it.Methods.Opening) == tf.Line(it.Methods.Closing) {
		// Spread it over lines, which gofmt then indents.
		last := it.Methods.List[len(it.Methods.List)-1]
		decls = append(decls, decl{file: pos.Filename, at: tf.Offset(it.Methods.Opening) + 1, text: "\n"},
			decl{file: pos.Filename, at: tf.Offset(last.End()), text: "\n" + sig + "\n", choice: choice})
	} else {
		at := tf.Offset(tf.LineStart(tf.Line(it.Methods.Closing)))
		decls = append(decls, decl{file: pos.Filename, at: at, text: gocode.IndentOf(src, at) + "\t" + sig + "\n", choice: choice})
	}
	for _, t := range implementers(st.ws, s.recv) {
		home := st.ws.Fset.Position(t.Obj().Pos()).Filename
		recv := receiver(st.pkg.TypesInfo, t, nil)
		tsig, err := st.signature(s, writer(t.Obj().Pkg().Path(), nil), map[string]bool{strings.Fields(recv)[0]: true})
		if err != nil {
			return nil, err
		}
		decls = append(decls, funcDecl(st.ws, home, "("+recv+") "+tsig))
	}
	return decls, nil
}

// interfaceType is the interface type declared by the type spec whose
// name is at offset in f.
func interfaceType(ws *workspace.Workspace, f *ast.File, offset int) *ast.InterfaceType {
	var it *ast.InterfaceType
	ast.Inspect(f, func(n ast.Node) bool {
		if ts, ok := n.(*ast.TypeSpec); ok && ws.Fset.Position(ts.Name.Pos()).Offset == offset {
			it, _ = ts.Type.(*ast.InterfaceType)
		}
		return it == nil
	})
	return it
}

// implementers returns the named types of the module, other than
// interfaces, that implement iface (directly or through a pointer), each
// once, in the order they are declared. Types are compared by package
// path, as the test variant of a package has types of its own.
func implementers(ws *workspace.Workspace, iface *types.Named) []*types.Named {
	want := methodKeys(iface)
	seen := map[string]bool{}
	var out []*types.Named
	for _, info := range ws.TypesInfos() {
		for _, obj := range info.Defs {
			tn, ok := obj.(*types.TypeName)
			if !ok || tn.IsAlias() || tn.Pkg() == nil || tn.Parent() != tn.Pkg().Scope() {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			pos := ws.Fset.Position(tn.Pos())
			if !ok || types.IsInterface(named) || seen[pos.String()] || !ws.Contains(pos.Filename) {
				continue
			}
			seen[pos.String()] = true
			have := methodKeys(types.NewPointer(named))
			if !slices.ContainsFunc(want, func(k string) bool { return !slices.Contains(have, k) }) {
				out = append(out, named)
			}
		}
	}
	slices.SortFunc(out, func(a, b *types.Named) int {
		pa, pb := ws.Fset.Position(a.Obj().Pos()), ws.Fset.Position(b.Obj().Pos())
		return cmp.Or(strings.Compare(pa.Filename, pb.Filename), pa.Offset-pb.Offset)
	})
	return out
}

// methodKeys are the methods of t's method set, each as its name and the
// types of its signature, written with package paths.
func methodKeys(t types.Type) []string {
	byPath := func(p *types.Package) string { return p.Path() }
	tuple := func(vs *types.Tuple) string {
		var ts []string
		for i := range vs.Len() {
			ts = append(ts, types.TypeString(vs.At(i).Type(), byPath))
		}
		return strings.Join(ts, ",")
	}
	ms := types.NewMethodSet(t)
	var keys []string
	for i := range ms.Len() {
		m := ms.At(i).Obj()
		sig := m.Type().(*types.Signature)
		keys = append(keys, fmt.Sprintf("%s(%s)%v(%s)", m.Name(), tuple(sig.Params()), sig.Variadic(), tuple(sig.Results())))
	}
	return keys
}

// funcDecl is a stub of the function with signature sig, at the end of
// file.
func funcDecl(ws *workspace.Workspace, file, sig string) decl {
	return decl{file: file, at: -1, text: "\nfunc " + sig + " {\n\tpanic(\"not implemented\")\n}\n", choice: fmt.Sprintf("func %s, in %s", sig, ws.Rel(file))}
}

// qualifier writes the types of the package at path unqualified, and
// those of others by package name.
func qualifier(path string) types.Qualifier {
	return func(p *types.Package) string {
		if p.Path() == path {
			return ""
		}
		return p.Name()
	}
}

// writer writes types for code in the package at path. For a method of
// an instance of a generic type, recv, it writes the instance's type
// arguments as the type's parameters: string as T for Stack[string].
func writer(path string, recv *types.Named) func(types.Type) string {
	qual := qualifier(path)
	var args []types.Type
	var names []string
	if recv != nil && recv.TypeArgs().Len() > 0 {
		names = typeParamNames(recv)
		for i := range recv.TypeArgs().Len() {
			args = append(args, recv.TypeArgs().At(i))
		}
	}
	var write func(types.Type) string
	write = func(t types.Type) string {
		for i, a := range args {
			if types.Identical(t, a) {
				return names[i]
			}
		}
		switch t := t.(type) {
		case *types.Pointer:
			return "*" + write(t.Elem())
		case *types.Slice:
			return "[]" + write(t.Elem())
		case *types.Array:
			return fmt.Sprintf("[%d]%s", t.Len(), write(t.Elem()))
		case *types.Map:
			return "map[" + write(t.Key()) + "]" + write(t.Elem())
		case *types.Named:
			if t.TypeArgs().Len() > 0 {
				var targs []string
				for i := range t.TypeArgs().Len() {
					targs = append(targs, write(t.TypeArgs().At(i)))
				}
				return qualified(t.Obj(), qual) + "[" + strings.Join(targs, ", ") + "]"
			}
		}
		return types.TypeString(t, qual)
	}
	return write
}

// qualified is obj's name, qualified by qual.
func qualified(obj types.Object, qual types.Qualifier) string {
	if p := qual(obj.Pkg()); p != "" {
		return p + "." + obj.Name()
	}
	return obj.Name()
}

// typeParamNames are the names named's methods give its type parameters,
// or else those of its declaration.
func typeParamNames(named *types.Named) []string {
	origin := named.Origin()
	tps := origin.TypeParams()
	for i := range origin.NumMethods() {
		if r := origin.Method(i).Type().(*types.Signature).RecvTypeParams(); r.Len() > 0 {
			tps = r
			break
		}
	}
	var names []string
	for i := range tps.Len() {
		names = append(names, tps.At(i).Obj().Name())
	}
	return names
}

// sigString writes sig's parameters and results, as they follow a
// function's name.
func sigString(sig *types.Signature, write func(types.Type) string) string {
	tuple := func(vs *types.Tuple, variadic bool) string {
		var ps []string
		for i := range vs.Len() {
			v := vs.At(i)
			t := write(v.Type())
			if variadic && i == vs.Len()-1 {
				t = "..." + write(v.Type().(*types.Slice).Elem())
			}
			if v.Name() != "" {
				t = v.Name() + " " + t
			}
			ps = append(ps, t)
		}
		return strings.Join(ps, ", ")
	}
	s := "(" + tuple(sig.Params(), sig.Variadic()) + ")"
	switch r := sig.Results(); {
	case r.Len() == 1 && r.At(0).Name() == "":
		s += " " + tuple(r, false)
	case r.Len() > 0:
		s += " (" + tuple(r, false) + ")"
	}
	return s
}

// fullName is "Name", or "Type.Name" for a method.
func (s stub) fullName() string {
	if s.recv != nil {
		return s.recv.Obj().Name() + "." + s.name
	}
	return s.name
}

// stub is a function to declare: its name, the package it goes in, and
// its first call.
type stub struct {
	name   string
	target *packages.Package
	call   *ast.CallExpr
	recv   *types.Named // for a method, the type it is a method of
	x      ast.Expr     // for a method, what it is called on
	method *types.Func  // for a method to implement an interface, the interface's
}

// undeclared returns the functions of the module file calls that are not
// declared, in the order of their first calls.
func undeclared(ws *workspace.Workspace, pkg *packages.Package, file *ast.File) []stub {
	info := pkg.TypesInfo
	seen := map[string]bool{}
	var out []stub
	add := func(key string, s stub) {
		if !seen[key] {
			seen[key] = true
			out = append(out, s)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		target, id := pkg, (*ast.Ident)(nil)
		switch f := call.Fun.(type) {
		case *ast.Ident:
			id = f
		case *ast.SelectorExpr:
			x, _ := f.X.(*ast.Ident)
			if pn, ok := info.Uses[x].(*types.PkgName); ok {
				target, id = pkg.Imports[pn.Imported().Path()], f.Sel
			} else if named := receiverType(ws, info, f); named != nil {
				if home, _ := ws.FileSyntax(ws.Fset.Position(named.Obj().Pos()).Filename); home != nil {
					add(types.TypeString(named.Origin(), nil)+"."+f.Sel.Name, stub{name: f.Sel.Name, target: home, call: call, recv: named, x: f.X})
				}
				return true
			}
		}
		if id == nil || target == nil || len(target.GoFiles) == 0 || !ws.Contains(target.GoFiles[0]) ||
			info.Uses[id] != nil || target.Types.Scope().Lookup(id.Name) != nil {
			return true
		}
		add(target.PkgPath+"."+id.Name, stub{name: id.Name, target: target, call: call})
		return true
	})
	return out
}

// unimplemented returns the methods types of the module lack where file
// uses one as an interface it does not implement (assigns, passes or
// returns it): one stub for each missing method.
func unimplemented(ws *workspace.Workspace, pkg *packages.Package, file *ast.File) []stub {
	info := pkg.TypesInfo
	seen := map[string]bool{}
	var out []stub
	var stack []ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		e, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		t := info.TypeOf(e)
		named := moduleType(ws, t)
		if named == nil || types.IsInterface(named) {
			return true
		}
		path := slices.Clone(stack)
		slices.Reverse(path)
		want := expected(info, path)
		if want == nil || !types.IsInterface(want) || types.AssignableTo(t, want) {
			return true
		}
		home, _ := ws.FileSyntax(ws.Fset.Position(named.Obj().Pos()).Filename)
		it := want.Underlying().(*types.Interface)
		for i := range it.NumMethods() {
			m := it.Method(i)
			if obj, _, _ := types.LookupFieldOrMethod(t, true, named.Obj().Pkg(), m.Name()); obj != nil || home == nil {
				continue
			}
			if k := types.TypeString(named.Origin(), nil) + "." + m.Name(); !seen[k] {
				seen[k] = true
				out = append(out, stub{name: m.Name(), target: home, recv: named, x: e, method: m})
			}
		}
		return true
	})
	return out
}

// moduleType is the named type of the module t is, or points to, if it
// is not generic; nil otherwise.
func moduleType(ws *workspace.Workspace, t types.Type) *types.Named {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil || !ws.Contains(ws.Fset.Position(named.Obj().Pos()).Filename) {
		return nil
	}
	return named
}

// receiverType is the named type of the module that sel's operand has,
// or points to, when sel names neither a field nor a method of it: the
// type to declare the method sel.Sel on. It is nil otherwise, and for
// empty interfaces and generic types.
func receiverType(ws *workspace.Workspace, info *types.Info, sel *ast.SelectorExpr) *types.Named {
	if info.Selections[sel] != nil {
		return nil
	}
	named := moduleType(ws, info.TypeOf(sel.X))
	if named == nil {
		return nil
	}
	if iface, ok := named.Underlying().(*types.Interface); ok && iface.NumMethods() == 0 {
		return nil // every type implements it: there is no telling which to add the method to
	}
	if obj, _, _ := types.LookupFieldOrMethod(named, true, named.Obj().Pkg(), sel.Sel.Name); obj != nil {
		return nil
	}
	return named
}

// receiver chooses the receiver of a new method of named, called on x,
// as "r T" or "r *T": as its methods have it (a pointer if any has one),
// or for a type without methods, a pointer if x is one, or is a variable
// or field of a struct type.
func receiver(info *types.Info, named *types.Named, x ast.Expr) string {
	name, ptr := "", false
	for i := range named.NumMethods() {
		r := named.Method(i).Type().(*types.Signature).Recv()
		if _, ok := r.Type().(*types.Pointer); ok {
			ptr = true
		}
		if name == "" && r.Name() != "_" {
			name = r.Name()
		}
	}
	if named.NumMethods() == 0 {
		_, ptr = info.TypeOf(x).(*types.Pointer)
		switch x.(type) {
		case *ast.Ident, *ast.SelectorExpr:
			_, isStruct := named.Underlying().(*types.Struct)
			ptr = ptr || isStruct
		}
	}
	if name == "" {
		name = strings.ToLower(named.Obj().Name()[:1])
	}
	t := named.Obj().Name()
	if tps := typeParamNames(named); len(tps) > 0 {
		t += "[" + strings.Join(tps, ", ") + "]"
	}
	if ptr {
		return name + " *" + t
	}
	return name + " " + t
}

// destination is the file a stub for target goes in: to_file if given;
// else home (for a method, the file of its type) if given; else the calling file, if it is of target and not a test; else the file
// of target the caller is named after (m.go for m_test.go), or its first.
func destination(ws *workspace.Workspace, target *packages.Package, caller, toFile, home string) (string, error) {
	var files []string
	for _, f := range target.GoFiles {
		if !strings.HasSuffix(f, "_test.go") {
			files = append(files, f)
		}
	}
	if toFile != "" {
		p := ws.Abs(toFile)
		if dpkg, f := ws.FileSyntax(p); f == nil || dpkg.PkgPath != target.PkgPath {
			return "", refactor.Refusef("to_file %s is not a file of package %s", toFile, target.PkgPath)
		}
		return p, nil
	}
	if home != "" {
		return home, nil
	}
	if len(files) == 0 {
		return "", refactor.Refusef("package %s has only test files; create a file for the stubs with create_file, then give it as to_file", target.PkgPath)
	}
	namesake := strings.TrimSuffix(caller, "_test.go") + ".go"
	for _, f := range files {
		if f == caller || f == namesake {
			return f, nil
		}
	}
	sort.Strings(files)
	return files[0], nil
}

// parameters declares a parameter for each argument of call: of its type,
// and named after it or its type.
func parameters(info *types.Info, call *ast.CallExpr, write func(types.Type) string, used map[string]bool) ([]string, error) {
	var out []string
	for i, arg := range call.Args {
		t := typeOf(info, arg)
		if t == nil {
			return nil, fmt.Errorf("cannot tell the type of argument %d, %s", i+1, types.ExprString(arg))
		}
		name := paramName(arg, t)
		for n := 2; used[name]; n++ {
			name = strings.TrimRight(name, "0123456789") + strconv.Itoa(n)
		}
		used[name] = true
		ts := write(t)
		if s, ok := t.Underlying().(*types.Slice); ok && i == len(call.Args)-1 && call.Ellipsis.IsValid() {
			ts = "..." + write(s.Elem())
		}
		out = append(out, name+" "+ts)
	}
	return out, nil
}

// paramName names a parameter after its argument (a variable or a field),
// or else after its type.
func paramName(arg ast.Expr, t types.Type) string {
	name := ""
	switch e := arg.(type) {
	case *ast.Ident:
		name = e.Name
	case *ast.SelectorExpr:
		name = e.Sel.Name
	}
	if name == "" || name == "_" {
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		switch t := t.(type) {
		case *types.Named:
			name = t.Obj().Name()
		case *types.Basic:
			switch info := t.Info(); {
			case info&types.IsInteger != 0:
				name = "n"
			case info&types.IsString != 0:
				name = "s"
			case info&types.IsBoolean != 0:
				name = "b"
			case info&types.IsFloat != 0:
				name = "f"
			}
		}
	}
	if name == "" {
		return "p"
	}
	name = strings.ToLower(name[:1]) + name[1:]
	if token.IsKeyword(name) || types.Universe.Lookup(name) != nil {
		return "p"
	}
	return name
}

// typeOf is the type of e, the default type if it is an untyped constant,
// or nil if it has none.
func typeOf(info *types.Info, e ast.Expr) types.Type {
	t := info.TypeOf(e)
	if lit, ok := e.(*ast.BasicLit); ok && t == nil {
		t = map[token.Token]types.Type{token.INT: types.Typ[types.Int], token.FLOAT: types.Typ[types.Float64], token.IMAG: types.Typ[types.Complex128],
			token.CHAR: types.Typ[types.Rune], token.STRING: types.Typ[types.String]}[lit.Kind]
	}
	if t == nil {
		return nil
	}
	t = types.Default(t)
	if b, ok := t.(*types.Basic); ok && (b.Kind() == types.Invalid || b.Kind() == types.UntypedNil) {
		return nil
	}
	return t
}

// results infers the results of the function call calls from how its
// value is used, as the text after the parameter list: "", " T" or
// " (T, U)". ok is false if the use does not tell.
func results(info *types.Info, file *ast.File, call *ast.CallExpr, write func(types.Type) string) (text string, ok bool) {
	path, _ := astutil.PathEnclosingInterval(file, call.Pos(), call.End())
	var ts []types.Type
	switch p := path[1].(type) {
	case *ast.ExprStmt:
		return "", true
	case *ast.AssignStmt:
		lhs := p.Lhs
		if len(p.Lhs) == len(p.Rhs) {
			lhs = p.Lhs[slices.Index(p.Rhs, ast.Expr(call)):][:1]
		}
		for i, l := range lhs {
			t := lhsType(info, file, p.Tok, l, len(lhs) > 1 && i == len(lhs)-1)
			if t == nil {
				return "", false
			}
			ts = append(ts, t)
		}
	case *ast.ReturnStmt:
		sig := enclosingSig(info, path)
		if sig == nil || len(p.Results) != 1 || sig.Results().Len() < 2 {
			if t := expected(info, path); t != nil {
				ts = append(ts, t)
				break
			}
			return "", false
		}
		for i := range sig.Results().Len() {
			ts = append(ts, sig.Results().At(i).Type())
		}
	default:
		t := expected(info, path)
		if t == nil {
			return "", false
		}
		ts = append(ts, t)
	}
	var names []string
	for _, t := range ts {
		names = append(names, write(t))
	}
	return resultText(strings.Join(names, ", ")), true
}

// resultText is the text after a parameter list for the result list r.
func resultText(r string) string {
	switch r = strings.TrimSpace(r); {
	case r == "":
		return ""
	case strings.Contains(r, ",") && !strings.HasPrefix(r, "("):
		return " (" + r + ")"
	}
	return " " + r
}

// lhsType is the type a call's result assigned (tok) to lhs must have:
// lhs's own type for =, else by its name (err, ok, or a blank last of
// several: an error), else by how the variable is used.
func lhsType(info *types.Info, file *ast.File, tok token.Token, lhs ast.Expr, lastOfSeveral bool) types.Type {
	id, isIdent := lhs.(*ast.Ident)
	switch {
	case tok == token.ASSIGN && !(isIdent && id.Name == "_"):
		return typeOf(info, lhs)
	case !isIdent:
		return nil
	case id.Name == "err", id.Name == "_" && lastOfSeveral:
		return types.Universe.Lookup("error").Type()
	case id.Name == "ok":
		return types.Typ[types.Bool]
	case id.Name == "_":
		return nil
	}
	obj := info.Defs[id]
	if obj == nil {
		return typeOf(info, id) // declared before, with :=
	}
	var uses []*ast.Ident
	for u, o := range info.Uses {
		if o == obj {
			uses = append(uses, u)
		}
	}
	slices.SortFunc(uses, func(a, b *ast.Ident) int { return int(a.Pos() - b.Pos()) })
	for _, u := range uses {
		path, _ := astutil.PathEnclosingInterval(file, u.Pos(), u.End())
		if t := expected(info, path); t != nil {
			return t
		}
	}
	return nil
}

// expected is the type the context of the expression path[0] needs it to
// have, or nil if it does not tell.
func expected(info *types.Info, path []ast.Node) types.Type {
	e := path[0].(ast.Expr)
	i := 1
	for ; i < len(path); i++ {
		p, ok := path[i].(*ast.ParenExpr)
		if !ok {
			break
		}
		e = p
	}
	if i == len(path) {
		return nil
	}
	boolean := types.Typ[types.Bool]
	switch p := path[i].(type) {
	case *ast.BinaryExpr:
		if p.Op == token.LAND || p.Op == token.LOR {
			return boolean
		}
		if p.X == e {
			return typeOf(info, p.Y)
		}
		return typeOf(info, p.X)
	case *ast.UnaryExpr:
		if p.Op == token.NOT {
			return boolean
		}
	case *ast.IfStmt:
		if p.Cond == e {
			return boolean
		}
	case *ast.ForStmt:
		if p.Cond == e {
			return boolean
		}
	case *ast.CallExpr:
		return argumentType(info, p, e)
	case *ast.ReturnStmt:
		sig := enclosingSig(info, path[i:])
		if j := slices.Index(p.Results, e); sig != nil && j >= 0 && len(p.Results) == sig.Results().Len() {
			return sig.Results().At(j).Type()
		}
	case *ast.AssignStmt:
		if j := slices.Index(p.Rhs, e); p.Tok == token.ASSIGN && j >= 0 && len(p.Lhs) == len(p.Rhs) {
			return typeOf(info, p.Lhs[j])
		}
	case *ast.ValueSpec:
		if p.Type != nil {
			return info.TypeOf(p.Type)
		}
	}
	return nil
}

// argumentType is the type of the parameter of call that e is the
// argument of, or nil if that says nothing (any).
func argumentType(info *types.Info, call *ast.CallExpr, e ast.Expr) types.Type {
	sig, ok := typeOf(info, call.Fun).(*types.Signature)
	j := slices.Index(call.Args, e)
	if !ok || j < 0 {
		return nil
	}
	var t types.Type
	switch n := sig.Params().Len(); {
	case sig.Variadic() && j >= n-1:
		t = sig.Params().At(n - 1).Type().(*types.Slice).Elem()
	case j < n:
		t = sig.Params().At(j).Type()
	}
	if t != nil && types.IsInterface(t) && t.Underlying().(*types.Interface).Empty() {
		return nil // any says nothing
	}
	return t
}

// enclosingSig is the signature of the function path is in.
func enclosingSig(info *types.Info, path []ast.Node) *types.Signature {
	for _, n := range path {
		switch f := n.(type) {
		case *ast.FuncLit:
			sig, _ := info.TypeOf(f).(*types.Signature)
			return sig
		case *ast.FuncDecl:
			if o := info.Defs[f.Name]; o != nil {
				return o.Type().(*types.Signature)
			}
			return nil
		}
	}
	return nil
}
