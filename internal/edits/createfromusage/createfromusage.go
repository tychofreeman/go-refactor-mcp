// Package createfromusage implements create_from_usage, a fix: it
// declares the functions code calls before they exist.
package createfromusage

import (
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
Use it after writing a test or code that calls functions not written yet: give the file with the calls. Every function it calls that is not declared gets a stub, or only those in names; a call pkg.F declares F in that package of the module, and a call x.M, where x is (or points to) a type of the module without a field or method M, declares the method M on that type, with a receiver like its other methods'.
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

// Plan checks preconditions and computes the edits.
func (*CreateFromUsage) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File    string            `json:"file"`
		Names   []string          `json:"names,omitempty"`
		Results map[string]string `json:"results,omitempty"`
		ToFile  string            `json:"to_file,omitempty"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	pkg, file := ws.FileSyntax(path)
	if file == nil {
		return nil, refactor.Refusef("file %s is not part of the loaded packages", a.File)
	}
	stubs := undeclared(ws, pkg, file)
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
		return nil, refactor.Refusef("%s calls no function that is not declared", a.File)
	}

	tf := ws.Fset.File(file.Pos())
	byFile := map[string]string{} // the stubs' declarations, by the file they go in
	var order, names, choices []string
	for _, s := range stubs {
		home, recv, used := "", "", map[string]bool{}
		if s.recv != nil {
			home = ws.Fset.Position(s.recv.Obj().Pos()).Filename
			recv = receiver(pkg.TypesInfo, s.recv, s.call.Fun.(*ast.SelectorExpr).X)
			used[strings.Fields(recv)[0]] = true
		}
		dest, err := destination(ws, s.target, path, a.ToFile, home)
		if err != nil {
			return nil, err
		}
		qual := func(p *types.Package) string {
			if p.Path() == s.target.PkgPath {
				return ""
			}
			return p.Name()
		}
		line := tf.Line(s.call.Pos())
		params, err := parameters(pkg.TypesInfo, s.call, qual, used)
		if err != nil {
			return nil, refactor.Refusef("for %s, called on line %d of %s: %v", s.name, line, a.File, err)
		}
		res, ok := a.Results[s.name]
		if ok {
			res = resultText(res)
		} else if res, ok = results(pkg.TypesInfo, file, s.call, qual); !ok {
			return nil, refactor.Refusef("cannot tell the results of %s from its use on line %d of %s; give them in results, e.g. {%q: \"(int, error)\"}", s.name, line, a.File, s.name)
		}
		sig := s.name + "(" + strings.Join(params, ", ") + ")" + res
		name := s.name
		if recv != "" {
			sig = "(" + recv + ") " + sig
			name = s.recv.Obj().Name() + "." + s.name
		}
		choices = append(choices, fmt.Sprintf("func %s, in %s", sig, ws.Rel(dest)))
		names = append(names, name)
		if _, ok := byFile[dest]; !ok {
			order = append(order, dest)
		}
		byFile[dest] += "\nfunc " + sig + " {\n\tpanic(\"not implemented\")\n}\n"
	}

	edits, err := writeStubs(order, ws, byFile)
	if err != nil {
		return nil, err
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("declared %s, as stubs", strings.Join(names, ", ")),
		Edits:   edits,
		Choices: choices,
	}, nil
}

// writeStubs appends the declarations of byFile to their files, in
// order, importing what they use.
func writeStubs(order []string, ws *workspace.Workspace, byFile map[string]string) ([]edit.FileEdit, error) {
	var edits []edit.FileEdit
	for _, dest := range order {
		dpkg, dfile := ws.FileSyntax(dest)
		src, err := os.ReadFile(dest)
		if err != nil {
			return nil, err
		}
		code, _, err := gocode.ParseDeclarations(byFile[dest])
		if err != nil {
			return nil, err
		}
		adder := imports.New(ws, dpkg, dfile, src)
		if _, err := gocode.AddImports(ws, dpkg, dfile, adder, dfile.Name.Pos(), code, nil); err != nil {
			return nil, err
		}
		edits = append(edits, edit.FileEdit{Path: dest, Edits: append([]edit.TextEdit{{Start: len(src), End: len(src), NewText: byFile[dest]}}, adder.Edits()...)})
	}
	return edits, nil
}

// stub is a function to declare: its name, the package it goes in, and
// its first call.
type stub struct {
	name   string
	target *packages.Package
	call   *ast.CallExpr
	recv   *types.Named // for a method, the type it is a method of
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
					add(types.TypeString(named, nil)+"."+f.Sel.Name, stub{name: f.Sel.Name, target: home, call: call, recv: named})
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

// receiverType is the named type of the module that sel's operand has,
// or points to, when sel names neither a field nor a method of it: the
// type to declare the method sel.Sel on. It is nil otherwise, and for
// interfaces and generic types.
func receiverType(ws *workspace.Workspace, info *types.Info, sel *ast.SelectorExpr) *types.Named {
	if info.Selections[sel] != nil {
		return nil
	}
	t := info.TypeOf(sel.X)
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil || types.IsInterface(named) || named.TypeParams().Len() > 0 ||
		!ws.Contains(ws.Fset.Position(named.Obj().Pos()).Filename) {
		return nil
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
	if ptr {
		return name + " *" + named.Obj().Name()
	}
	return name + " " + named.Obj().Name()
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
func parameters(info *types.Info, call *ast.CallExpr, qual types.Qualifier, used map[string]bool) ([]string, error) {
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
		ts := types.TypeString(t, qual)
		if s, ok := t.Underlying().(*types.Slice); ok && i == len(call.Args)-1 && call.Ellipsis.IsValid() {
			ts = "..." + types.TypeString(s.Elem(), qual)
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
func results(info *types.Info, file *ast.File, call *ast.CallExpr, qual types.Qualifier) (text string, ok bool) {
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
		names = append(names, types.TypeString(t, qual))
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
