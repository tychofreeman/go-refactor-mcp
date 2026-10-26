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
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
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
	return `Declare as stubs (bodies of panic("not implemented")) the functions, methods, types and fields that code uses before they exist, so that a test written first compiles and fails: a test with if got := (&Cart{}).Total(); got != 0 gets func (c *Cart) Total() int declared in cart.go. An edit, and one that may start from code that does not type-check, when it leaves fewer errors and no new one. Other edits declare such stubs on their own; call it to choose names, results or the file.
From how the code uses them it declares: functions (pkg.F in that package of the module); methods on types of the module, and on every type implementing an interface it adds one to; the methods a type lacks where it is used as an interface (var _ io.Writer = (*T)(nil)); struct types from keyed literals (T{name: v}); fields a struct lacks (x.F, T{F: v}). Parameters take their arguments' types and names; results come from the use (err is an error, a value compared with 0 an int), or from results.
Give file, the file with the uses; names limits it to those, to_file sets where stubs go. The result's "chose:" lines give each signature and file: check them.`
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
	stubs := append(undeclaredTypes(pkg, file), undeclared(ws, pkg, file)...)
	stubs = append(stubs, undeclaredFields(ws, pkg, file)...)
	stubs = append(stubs, unimplemented(ws, pkg, file)...)
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

	st := &stubber{Site: site.Site{WS: ws, Pkg: pkg, File: file}, caller: path, args: a}
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
	edits, err := writeStubs(ws, decls, st.imports)
	if err != nil {
		return nil, err
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("declared %s, as stubs", strings.Join(names, ", ")),
		Edits:   edits,
		Choices: choices,
	}, nil
}

// writeStubs writes decls into their files, importing what they use:
// by the paths in paths for each file, where a name could mean several.
func writeStubs(ws *workspace.Workspace, decls []decl, paths map[string][]string) ([]edit.FileEdit, error) {
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
			kind := "interface"
			if d.field {
				kind = "struct"
			}
			all += "\ntype _ " + kind + " {\n" + d.text + "}\n"
		}
		if end != "" {
			tes = append(tes, edit.TextEdit{Start: len(src), End: len(src), NewText: end})
		}
		code, _, err := gocode.ParseDeclarations(all)
		if err != nil {
			return nil, err
		}
		adder := imports.New(ws, dpkg, dfile, src)
		if _, err := gocode.AddImports(ws, dpkg, dfile, adder, dfile.Name.Pos(), code, paths[dest]); err != nil {
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
	field  bool // text is fields of a struct, not methods of an interface
}

// stubber declares the stubs for the calls of one file.
type stubber struct {
	site.Site
	caller  string // its path
	args    args
	imports map[string][]string // by destination file: the packages its stubs' types are from
	spread  map[token.Pos]bool  // one-line structs a field stub has spread over lines, by opening brace
}

// writer is writer for code in the file dest, of the package at path:
// a package dest imports is written by the name it imports it as, and
// each package written is noted, so that dest imports that one.
func (st *stubber) writer(dest, path string, recv *types.Named) func(types.Type) string {
	names := map[string]string{}
	if pkg, f := st.WS.FileSyntax(dest); f != nil {
		for _, spec := range f.Imports {
			p, _ := strconv.Unquote(spec.Path.Value)
			switch {
			case spec.Name != nil && spec.Name.Name != "_" && spec.Name.Name != ".":
				names[p] = spec.Name.Name
			case spec.Name == nil && pkg.Imports[p] != nil:
				names[p] = pkg.Imports[p].Name
			}
		}
	}
	if st.imports == nil {
		st.imports = map[string][]string{}
	}
	return writer(func(p *types.Package) string {
		if p.Path() == path {
			return ""
		}
		if !slices.Contains(st.imports[dest], p.Path()) {
			st.imports[dest] = append(st.imports[dest], p.Path())
		}
		if n, ok := names[p.Path()]; ok {
			return n
		}
		return p.Name()
	}, recv)
}

// declare returns the declarations s needs.
func (st *stubber) declare(s stub) ([]decl, error) {
	switch {
	case s.typ:
		return st.declareType(s)
	case s.field:
		return st.declareField(s)
	case s.method != nil:
		return st.implement(s)
	case s.recv != nil && types.IsInterface(s.recv):
		return st.extend(s)
	}
	home, recv, used := "", "", map[string]bool{}
	if s.recv != nil {
		home = st.WS.Fset.Position(s.recv.Obj().Pos()).Filename
		recv = receiver(st.Pkg.TypesInfo, s.recv, s.x)
		used[strings.Fields(recv)[0]] = true
	}
	dest, err := destination(st.WS, s.target, st.caller, st.args.ToFile, home)
	if err != nil {
		return nil, err
	}
	sig, err := st.signature(s, st.writer(dest, s.target.PkgPath, s.recv), used)
	if err != nil {
		return nil, err
	}
	if recv != "" {
		sig = "(" + recv + ") " + sig
	}
	return []decl{funcDecl(st.WS, dest, sig)}, nil
}

// declareType declares s, a type, as a struct with the fields its keyed
// literals give, typed as their values are.
func (st *stubber) declareType(s stub) ([]decl, error) {
	dest, err := destination(st.WS, s.target, st.caller, st.args.ToFile, "")
	if err != nil {
		return nil, err
	}
	write := st.writer(dest, s.target.PkgPath, nil)
	var names []string
	fieldTypes := map[string]string{}
	for _, lit := range s.lits {
		for _, e := range lit.Elts {
			kv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if _, seen := fieldTypes[key.Name]; !ok || seen {
				continue
			}
			t := typeOf(st.Pkg.TypesInfo, kv.Value)
			if t == nil {
				return nil, refactor.Refusef("cannot tell the type of field %s of %s from %s; declare %s with add_declaration", key.Name, s.name, types.ExprString(kv.Value), s.name)
			}
			names = append(names, key.Name)
			fieldTypes[key.Name] = write(t)
		}
	}
	if len(s.lits) == 0 {
		return []decl{{file: dest, at: -1, text: fmt.Sprintf("\ntype %s struct{}\n", s.name), choice: fmt.Sprintf("type %s struct{}, in %s: it is only named as a type; add its fields", s.name, st.WS.Rel(dest))}}, nil
	}
	if len(names) == 0 {
		return nil, refactor.Refusef("cannot tell the fields of %s: no literal of it names them; write one with field names (%s{name: value}), or declare %s with add_declaration", s.name, s.name, s.name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\ntype %s struct {\n", s.name)
	for _, n := range names {
		fmt.Fprintf(&b, "\t%s %s\n", n, fieldTypes[n])
	}
	b.WriteString("}\n")
	return []decl{{file: dest, at: -1, text: b.String(), choice: fmt.Sprintf("type %s struct with %s, in %s", s.name, strings.Join(names, ", "), st.WS.Rel(dest))}}, nil
}

// declareField adds s, a field, to its struct, s.recv.
func (st *stubber) declareField(s stub) ([]decl, error) {
	obj := s.recv.Obj()
	var have []string
	if st, ok := s.recv.Underlying().(*types.Struct); ok {
		for i := range st.NumFields() {
			have = append(have, st.Field(i).Name())
		}
	}
	for i := range s.recv.NumMethods() {
		have = append(have, s.recv.Method(i).Name())
	}
	for _, n := range have {
		if refactor.Close(n, s.name) {
			return nil, refactor.Refusef("%s is close to %s, which %s has; if %s is meant to be new, add it with add_field", s.name, n, obj.Name(), s.name)
		}
	}
	if s.ftype == nil {
		return nil, refactor.Refusef("cannot tell the type of field %s of %s from how %s uses it; add it with add_field", s.name, obj.Name(), st.args.File)
	}
	pos := st.WS.Fset.Position(obj.Pos())
	_, f := st.WS.FileSyntax(pos.Filename)
	var fields *ast.FieldList
	ast.Inspect(f, func(n ast.Node) bool {
		if ts, ok := n.(*ast.TypeSpec); ok && ts.Name.Pos() == obj.Pos() {
			if t, ok := ts.Type.(*ast.StructType); ok {
				fields = t.Fields
			}
		}
		return fields == nil
	})
	if fields == nil {
		return nil, refactor.Refusef("cannot find the declaration of struct %s", obj.Name())
	}
	tf := st.WS.Fset.File(f.Pos())
	src, err := os.ReadFile(pos.Filename)
	if err != nil {
		return nil, err
	}
	field := s.name + " " + st.writer(pos.Filename, obj.Pkg().Path(), nil)(s.ftype)
	choice := fmt.Sprintf("field %s, in struct %s in %s", field, obj.Name(), st.WS.Rel(pos.Filename))
	if tf.Line(fields.Opening) != tf.Line(fields.Closing) {
		at := tf.Offset(tf.LineStart(tf.Line(fields.Closing)))
		return []decl{{file: pos.Filename, at: at, text: gocode.IndentOf(src, at) + "\t" + field + "\n", choice: choice, field: true}}, nil
	}
	// A struct on one line: spread it over lines, which gofmt then indents,
	// once for all its new fields.
	at := tf.Offset(fields.Opening) + 1
	if n := len(fields.List); n > 0 {
		at = tf.Offset(fields.List[n-1].End())
	}
	text := field + "\n"
	if st.spread == nil {
		st.spread = map[token.Pos]bool{}
	}
	if !st.spread[fields.Opening] {
		st.spread[fields.Opening] = true
		text = "\n" + text
		if len(fields.List) > 0 {
			return []decl{{file: pos.Filename, at: tf.Offset(fields.Opening) + 1, text: "\n", field: true}, {file: pos.Filename, at: at, text: text, choice: choice, field: true}}, nil
		}
	}
	return []decl{{file: pos.Filename, at: at, text: text, choice: choice, field: true}}, nil
}

// signature is that of s's function, from its call: "Name(params)
// results".
func (st *stubber) signature(s stub, write func(types.Type) string, used map[string]bool) (string, error) {
	if s.sig != nil {
		return s.name + sigString(s.sig, write), nil
	}
	line := st.WS.Fset.Position(s.call.Pos()).Line
	params, err := parameters(st.Pkg.TypesInfo, s.call, write, used)
	if err != nil {
		return "", refactor.Refusef("for %s, called on line %d of %s: %v", s.name, line, st.args.File, err)
	}
	res, ok := st.args.Results[s.name]
	if ok {
		res = resultText(res)
	} else if res, ok = results(st.Pkg.TypesInfo, st.File, s.call, write); !ok {
		return "", refactor.Refusef("cannot tell the results of %s from its use on line %d of %s; give them in results, e.g. {%q: \"(int, error)\"}", s.name, line, st.args.File, s.name)
	}
	return s.name + "(" + strings.Join(params, ", ") + ")" + res, nil
}

// implement declares s.method, of an interface, on s.recv, with the
// interface's signature.
func (st *stubber) implement(s stub) ([]decl, error) {
	home := st.WS.Fset.Position(s.recv.Obj().Pos()).Filename
	dest, err := destination(st.WS, s.target, st.caller, st.args.ToFile, home)
	if err != nil {
		return nil, err
	}
	sig := sigString(s.method.Type().(*types.Signature), st.writer(dest, s.target.PkgPath, s.recv))
	recv := receiver(st.Pkg.TypesInfo, s.recv, s.x)
	return []decl{funcDecl(st.WS, dest, "("+recv+") "+s.name+sig)}, nil
}

// extend adds the method s calls to the interface s.recv, and a stub of it
// to each type of the module that implemented the interface, so that it
// still does.
func (st *stubber) extend(s stub) ([]decl, error) {
	obj := s.recv.Obj()
	pos := st.WS.Fset.Position(obj.Pos())
	opened, err := site.At(st.WS, pos.Filename)
	if err != nil {
		return nil, err
	}
	f, tf, src := opened.File, opened.TF, opened.Src
	it := interfaceType(st.WS, f, pos.Offset)
	if it == nil {
		return nil, refactor.Refusef("cannot find the declaration of interface %s", obj.Name())
	}
	sig, err := st.signature(s, st.writer(pos.Filename, obj.Pkg().Path(), s.recv), map[string]bool{})
	if err != nil {
		return nil, err
	}
	choice := fmt.Sprintf("%s, in interface %s in %s", sig, obj.Name(), st.WS.Rel(pos.Filename))
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
	for _, t := range implementers(st.WS, s.recv) {
		home := st.WS.Fset.Position(t.Obj().Pos()).Filename
		recv := receiver(st.Pkg.TypesInfo, t, nil)
		tsig, err := st.signature(s, st.writer(home, t.Obj().Pkg().Path(), nil), map[string]bool{strings.Fields(recv)[0]: true})
		if err != nil {
			return nil, err
		}
		decls = append(decls, funcDecl(st.WS, home, "("+recv+") "+tsig))
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

// writer writes types, qualifying packages by qual. For a method of an
// instance of a generic type, recv, it writes the instance's type
// arguments as the type's parameters: string as T for Stack[string].
func writer(qual types.Qualifier, recv *types.Named) func(types.Type) string {
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
	recv   *types.Named     // for a method, the type it is a method of
	x      ast.Expr         // for a method, what it is called on
	method *types.Func      // for a method to implement an interface, the interface's
	sig    *types.Signature // for a function or method passed as a value, the type the call it is passed to needs
	typ    bool             // a type, not a function: a struct with the fields its literals, lits, name
	lits   []*ast.CompositeLit
	field  bool // a field of recv, a struct, of type ftype (nil if its use does not tell)
	ftype  types.Type
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
		for _, arg := range call.Args {
			if key, s, ok := valueStub(ws, pkg, call, arg); ok {
				add(key, s)
			}
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

// undeclaredTypes returns the types file makes values of with composite
// literals, T{...} or the elements of []T{...}, that nothing declares.
func undeclaredTypes(pkg *packages.Package, file *ast.File) []stub {
	info := pkg.TypesInfo
	undefined := func(e ast.Expr) *ast.Ident {
		if s, ok := e.(*ast.StarExpr); ok {
			e = s.X
		}
		id, ok := e.(*ast.Ident)
		if !ok || info.Uses[id] != nil || pkg.Types.Scope().Lookup(id.Name) != nil || types.Universe.Lookup(id.Name) != nil {
			return nil
		}
		return id
	}
	byName := map[string]int{}
	var out []stub
	note := func(id *ast.Ident, lit *ast.CompositeLit) {
		i, ok := byName[id.Name]
		if !ok {
			i = len(out)
			byName[id.Name] = i
			out = append(out, stub{name: id.Name, target: pkg, typ: true})
		}
		if lit != nil {
			out[i].lits = append(out[i].lits, lit)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		if f, ok := n.(*ast.Field); ok {
			t := f.Type
			for {
				switch e := t.(type) {
				case *ast.StarExpr:
					t = e.X
					continue
				case *ast.ArrayType:
					t = e.Elt
					continue
				case *ast.MapType:
					t = e.Value
					continue
				case *ast.Ellipsis:
					t = e.Elt
					continue
				}
				break
			}
			if id := undefined(t); id != nil {
				note(id, nil)
			}
			return true
		}
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if id := undefined(lit.Type); id != nil {
			note(id, lit)
			return true
		}
		var elt ast.Expr
		switch t := lit.Type.(type) {
		case *ast.ArrayType:
			elt = t.Elt
		case *ast.MapType:
			elt = t.Value
		}
		if id := undefined(elt); id != nil {
			note(id, nil)
			for _, e := range lit.Elts {
				if kv, ok := e.(*ast.KeyValueExpr); ok {
					e = kv.Value
				}
				if el, ok := e.(*ast.CompositeLit); ok && el.Type == nil {
					note(id, el)
				}
			}
		}
		return true
	})
	return out
}

// undeclaredFields returns the fields file uses of the module's struct
// types that they lack: read or assigned as x.F, or set in a literal
// T{F: v}; each typed by its first use that tells.
func undeclaredFields(ws *workspace.Workspace, pkg *packages.Package, file *ast.File) []stub {
	info := pkg.TypesInfo
	called := map[ast.Expr]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			called[c.Fun] = true
		}
		return true
	})
	byKey := map[string]int{}
	var out []stub
	note := func(named *types.Named, name string, t types.Type) {
		home, _ := ws.FileSyntax(ws.Fset.Position(named.Obj().Pos()).Filename)
		if home == nil {
			return
		}
		key := types.TypeString(named, nil) + "." + name
		if i, ok := byKey[key]; ok {
			if out[i].ftype == nil {
				out[i].ftype = t
			}
			return
		}
		byKey[key] = len(out)
		out = append(out, stub{name: name, target: home, recv: named, field: true, ftype: t})
	}
	struct_ := func(named *types.Named) bool {
		_, ok := named.Underlying().(*types.Struct)
		return ok && named.TypeArgs().Len() == 0
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if named := receiverType(ws, info, n); named != nil && !called[n] && struct_(named) {
				t := fieldUse(info, file, n)
				if t != nil {
					if _, fn := t.Underlying().(*types.Signature); fn {
						return true // a method passed as a value: valueStub's
					}
				}
				note(named, n.Sel.Name, t)
			}
		case *ast.CompositeLit:
			named := moduleType(ws, info.TypeOf(n))
			if named == nil || !struct_(named) {
				return true
			}
			for _, e := range n.Elts {
				kv, ok := e.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok {
					if obj, _, _ := types.LookupFieldOrMethod(named, false, named.Obj().Pkg(), key.Name); obj == nil {
						note(named, key.Name, typeOf(info, kv.Value))
					}
				}
			}
		}
		return true
	})
	return out
}

// fieldUse is the type a field sel must have for how it is used: what
// is assigned to it, or what its context needs; nil if that does not
// tell.
func fieldUse(info *types.Info, file *ast.File, sel *ast.SelectorExpr) types.Type {
	path, _ := astutil.PathEnclosingInterval(file, sel.Pos(), sel.End())
	if len(path) < 2 || path[0] != sel {
		return nil
	}
	if a, ok := path[1].(*ast.AssignStmt); ok && len(a.Lhs) == len(a.Rhs) {
		if j := slices.Index(a.Lhs, ast.Expr(sel)); j >= 0 {
			return typeOf(info, a.Rhs[j])
		}
	}
	return expected(info, path)
}

// valueStub is the stub for arg, an argument of call, if it names a
// function or method of the module that is not declared, passed as a
// value where call needs a function: its key (as undeclared keys them)
// and the stub, with the signature call needs.
func valueStub(ws *workspace.Workspace, pkg *packages.Package, call *ast.CallExpr, arg ast.Expr) (string, stub, bool) {
	info := pkg.TypesInfo
	t := argumentType(info, call, arg)
	if t == nil {
		return "", stub{}, false
	}
	sig, ok := t.Underlying().(*types.Signature)
	if !ok {
		return "", stub{}, false
	}
	target, id := pkg, (*ast.Ident)(nil)
	switch a := arg.(type) {
	case *ast.Ident:
		id = a
	case *ast.SelectorExpr:
		x, _ := a.X.(*ast.Ident)
		if pn, ok := info.Uses[x].(*types.PkgName); ok {
			target, id = pkg.Imports[pn.Imported().Path()], a.Sel
			break
		}
		named := receiverType(ws, info, a)
		if named == nil {
			return "", stub{}, false
		}
		home, _ := ws.FileSyntax(ws.Fset.Position(named.Obj().Pos()).Filename)
		if home == nil {
			return "", stub{}, false
		}
		return types.TypeString(named.Origin(), nil) + "." + a.Sel.Name, stub{name: a.Sel.Name, target: home, call: call, recv: named, x: a.X, sig: sig}, true
	}
	if id == nil || target == nil || len(target.GoFiles) == 0 || !ws.Contains(target.GoFiles[0]) ||
		info.Uses[id] != nil || target.Types.Scope().Lookup(id.Name) != nil || types.Universe.Lookup(id.Name) != nil {
		return "", stub{}, false
	}
	return target.PkgPath + "." + id.Name, stub{name: id.Name, target: target, call: call, sig: sig}, true
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

// paramName names a parameter after its argument (a variable or a field,
// or one element of a slice named in the plural: id for ids[0]), or else
// after its type.
func paramName(arg ast.Expr, t types.Type) string {
	name := ""
	switch e := ast.Unparen(arg).(type) {
	case *ast.Ident:
		name = e.Name
	case *ast.SelectorExpr:
		name = e.Sel.Name
	case *ast.IndexExpr:
		if id, ok := e.X.(*ast.Ident); ok && len(id.Name) > 1 && strings.HasSuffix(id.Name, "s") {
			name = strings.TrimSuffix(id.Name, "s")
		}
	}
	if name == "" || name == "_" {
		name = typeName(t)
	}
	name = strings.ToLower(name[:1]) + name[1:]
	if token.IsKeyword(name) || types.Universe.Lookup(name) != nil {
		return "v"
	}
	return name
}

// typeName is a parameter name for a value of type t, as Go code names
// one: config for a *Config, configs for a []Config, n for an int, err,
// m for a map, ch, fn; else v.
func typeName(t types.Type) string {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if types.Identical(t, types.Universe.Lookup("error").Type()) {
		return "err"
	}
	switch t := t.(type) {
	case *types.Named:
		return t.Obj().Name()
	case *types.Basic:
		switch info := t.Info(); {
		case info&types.IsInteger != 0:
			return "n"
		case info&types.IsString != 0:
			return "s"
		case info&types.IsBoolean != 0:
			return "b"
		case info&types.IsFloat != 0:
			return "f"
		}
	case *types.Slice:
		return plural(t.Elem())
	case *types.Array:
		return plural(t.Elem())
	case *types.Map:
		return "m"
	case *types.Chan:
		return "ch"
	case *types.Signature:
		return "fn"
	}
	return "v"
}

// plural names several values of type elem: configs, else xs.
func plural(elem types.Type) string {
	if n, ok := elem.(*types.Named); ok {
		return n.Obj().Name() + "s"
	}
	return "xs"
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
	case *ast.KeyValueExpr:
		if lit, ok := path[min(i+1, len(path)-1)].(*ast.CompositeLit); ok && p.Value == e {
			return elementType(info, lit, p.Key)
		}
	case *ast.CompositeLit:
		return elementType(info, p, nil)
	case *ast.CaseClause:
		// A case of a switch: compared with its tag, or else a condition.
		if i+2 < len(path) && slices.Contains(p.List, e) {
			if sw, ok := path[i+2].(*ast.SwitchStmt); ok {
				if sw.Tag == nil {
					return boolean
				}
				return typeOf(info, sw.Tag)
			}
		}
	}
	return nil
}

// elementType is the type an element of the composite literal lit must
// have: given key, that of the struct field it names or a map's value;
// else a slice's or array's element. It is nil where that says nothing:
// an unkeyed struct element, or any.
func elementType(info *types.Info, lit *ast.CompositeLit, key ast.Expr) types.Type {
	t := info.TypeOf(lit)
	if t == nil {
		return nil
	}
	var elem types.Type
	switch u := t.Underlying().(type) {
	case *types.Struct:
		if id, ok := key.(*ast.Ident); ok {
			for f := range u.Fields() {
				if f.Name() == id.Name {
					elem = f.Type()
				}
			}
		}
	case *types.Slice:
		elem = u.Elem()
	case *types.Array:
		elem = u.Elem()
	case *types.Map:
		if key != nil {
			elem = u.Elem()
		}
	}
	if elem != nil && types.IsInterface(elem) && elem.Underlying().(*types.Interface).Empty() {
		return nil // any says nothing
	}
	return elem
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
	if sig.TypeParams().Len() > 0 && mentionsTypeParam(t) {
		return nil // the call's type arguments could not be inferred: its own type parameter says nothing
	}
	return t
}

// mentionsTypeParam reports whether t is or contains a type parameter.
func mentionsTypeParam(t types.Type) bool {
	switch t := t.(type) {
	case *types.TypeParam:
		return true
	case *types.Pointer:
		return mentionsTypeParam(t.Elem())
	case *types.Slice:
		return mentionsTypeParam(t.Elem())
	case *types.Array:
		return mentionsTypeParam(t.Elem())
	case *types.Chan:
		return mentionsTypeParam(t.Elem())
	case *types.Map:
		return mentionsTypeParam(t.Key()) || mentionsTypeParam(t.Elem())
	case *types.Named:
		for a := range t.TypeArgs().Types() {
			if mentionsTypeParam(a) {
				return true
			}
		}
	case *types.Signature:
		for _, tup := range []*types.Tuple{t.Params(), t.Results()} {
			for v := range tup.Variables() {
				if mentionsTypeParam(v.Type()) {
					return true
				}
			}
		}
	}
	return false
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
