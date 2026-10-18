// Package rename implements a type-aware rename of any identifier declared
// in the workspace: variables, constants, functions, types, type
// parameters, struct fields and methods.
//
// Correctness comes from go/types: every reference is found through
// types.Info (not by text search), and the new name is checked against
// Go's scoping rules so that no reference changes meaning.
package rename

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// Rename is the "rename" refactoring.
type Rename struct{}

// New returns the rename refactoring.
func New() *Rename { return &Rename{} }

func (*Rename) Name() string { return "rename" }

func (*Rename) Description() string {
	return `Rename a variable, constant, function, type, type parameter, struct field or method, updating every reference in the workspace.
Use it to give a symbol a clearer name. Identify the symbol by file, line and its current name (add column only if the name occurs more than once on that line with different meanings).
Refuses if the new name would clash with or shadow another symbol, if an exported name used by another package would become unexported, if the method implements an interface, or if the symbol is declared outside the workspace.
A doc comment that starts with the old name (the Go convention) is renamed too; other comments that mention it are listed in a note (for a local, only those in its scope).`
}

func (*Rename) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":     {"type": "string",  "description": "Path of a file containing the symbol (declaration or any use), relative to the workspace root."},
    "line":     {"type": "integer", "description": "1-based line number of that occurrence."},
    "name":     {"type": "string",  "description": "Current name of the symbol."},
    "column":   {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "new_name": {"type": "string",  "description": "The new name. Must be a valid Go identifier."}
  },
  "required": ["file", "line", "name", "new_name"],
  "additionalProperties": false
}`)
}

type args struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column,omitempty"`
	Name    string `json:"name"`
	NewName string `json:"new_name"`
}

// Plan checks preconditions and computes the edits.
func (*Rename) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.NewName == "" {
		return nil, refactor.Refusef("new_name is required")
	}
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	obj := objectOf(pkg.TypesInfo, id)
	if obj == nil {
		if id.Name == "_" {
			return nil, refactor.Refusef("the blank identifier _ cannot be renamed")
		}
		return nil, refactor.Refusef("%q at %s does not denote a renamable symbol (package names are not supported)", id.Name, ws.Position(id.Pos()))
	}

	r := &renamer{ws: ws, old: obj.Name(), new: a.NewName, key: keyOf(ws.Fset, obj), first: obj}
	if err := r.checkObject(); err != nil {
		return nil, err
	}
	r.collect()
	for _, check := range []func() error{
		r.checkNewName,
		r.checkExport,
		r.checkLexical,
		r.checkFieldOrMethod,
		r.checkEmbeddedType,
	} {
		if err := check(); err != nil {
			return nil, err
		}
	}
	return r.plan()
}

// objectOf is ObjectOf extended to the symbolic variable of a type switch
// (`switch x := v.(type)`), which has no object of its own: each case
// clause gets an implicit object declared at the same position.
func objectOf(info *types.Info, id *ast.Ident) types.Object {
	if obj := info.ObjectOf(id); obj != nil {
		return obj
	}
	for n, obj := range info.Implicits {
		if _, ok := n.(*ast.CaseClause); ok && obj.Pos() == id.Pos() {
			return obj
		}
	}
	return nil
}

type ref struct {
	pkg *packages.Package
	id  *ast.Ident
}

type renamer struct {
	ws       *workspace.Workspace
	old, new string
	key      string       // identity of the symbol across package variants
	first    types.Object // the object found at the requested position
	targets  []types.Object
	refs     []ref
}

// keyOf identifies an object by its declaration position. The same
// declaration yields distinct types.Objects in a package and in its test
// variant, and generic instantiations yield copies of fields and methods;
// the position (of the origin) is what they have in common.
func keyOf(fset *token.FileSet, obj types.Object) string {
	switch o := obj.(type) {
	case *types.Var:
		obj = o.Origin()
	case *types.Func:
		obj = o.Origin()
	}
	p := fset.Position(obj.Pos())
	return fmt.Sprintf("%s:%d:%d:%s", p.Filename, p.Line, p.Column, obj.Name())
}

func (r *renamer) checkObject() error {
	obj, ws := r.first, r.ws
	switch o := obj.(type) {
	case *types.PkgName:
		return refactor.Refusef("renaming imports is not supported yet")
	case *types.Label:
		return refactor.Refusef("renaming labels is not supported yet")
	case *types.Builtin, *types.Nil:
		return refactor.Refusef("%q is a predeclared Go identifier and cannot be renamed", obj.Name())
	case *types.Var:
		if o.Embedded() {
			return refactor.Refusef("%q is an embedded field, whose name comes from its type; renaming embedded fields is not supported yet", obj.Name())
		}
	case *types.Func:
		if recv := o.Signature().Recv(); recv != nil && types.IsInterface(recv.Type()) {
			return refactor.Refusef("%q is an interface method; renaming interface methods (and all their implementations) is not supported yet", obj.Name())
		}
	}
	if obj.Pkg() == nil {
		return refactor.Refusef("%q is a predeclared Go identifier and cannot be renamed", obj.Name())
	}
	declFile := ws.Fset.Position(obj.Pos()).Filename
	if declFile == "" || !ws.Contains(declFile) {
		return refactor.Refusef("%q is declared in package %s, outside the workspace (standard library or a dependency); only symbols declared in this module can be renamed",
			obj.Name(), obj.Pkg().Path())
	}
	if obj.Name() == "_" {
		return refactor.Refusef("the blank identifier _ cannot be renamed")
	}
	if isPackageLevel(obj) && obj.Name() == "init" {
		return refactor.Refusef("init functions cannot be renamed")
	}
	if isPackageLevel(obj) && obj.Name() == "main" && obj.Pkg().Name() == "main" {
		return refactor.Refusef("func main in package main cannot be renamed")
	}
	return nil
}

// collect finds every object and reference with the target's key, across
// all package variants.
func (r *renamer) collect() {
	seenObj := map[types.Object]bool{}
	addObj := func(o types.Object) {
		if !seenObj[o] {
			seenObj[o] = true
			r.targets = append(r.targets, o)
		}
	}
	for _, p := range r.ws.Pkgs {
		info := p.TypesInfo
		if info == nil {
			continue
		}
		for _, m := range []map[*ast.Ident]types.Object{info.Defs, info.Uses} {
			for id, o := range m {
				if o != nil && keyOf(r.ws.Fset, o) == r.key {
					r.refs = append(r.refs, ref{p, id})
					addObj(origin(o))
				}
			}
		}
		for _, o := range info.Implicits {
			if keyOf(r.ws.Fset, o) == r.key {
				addObj(o)
			}
		}
	}
	sort.Slice(r.refs, func(i, j int) bool {
		pi, pj := r.ws.Fset.Position(r.refs[i].id.Pos()), r.ws.Fset.Position(r.refs[j].id.Pos())
		if pi.Filename != pj.Filename {
			return pi.Filename < pj.Filename
		}
		return pi.Offset < pj.Offset
	})
}

func origin(o types.Object) types.Object {
	switch o := o.(type) {
	case *types.Var:
		return o.Origin()
	case *types.Func:
		return o.Origin()
	}
	return o
}

func (r *renamer) checkNewName() error {
	switch {
	case !token.IsIdentifier(r.new):
		return refactor.Refusef("%q is not a valid Go identifier", r.new)
	case r.new == "_":
		return refactor.Refusef("renaming to _ would delete the symbol; that is not a rename")
	case r.new == r.old:
		return refactor.Refusef("the symbol is already called %q", r.old)
	}
	if isPackageLevel(r.first) {
		if r.new == "init" {
			return refactor.Refusef("init is reserved for package initialization functions")
		}
		if r.new == "main" && r.first.Pkg().Name() == "main" {
			return refactor.Refusef("main is reserved in package main")
		}
	}
	return nil
}

// checkExport refuses to unexport a symbol that another package uses.
func (r *renamer) checkExport() error {
	if !token.IsExported(r.old) || token.IsExported(r.new) {
		return nil
	}
	home := r.first.Pkg().Path()
	for _, rf := range r.refs {
		if rf.pkg.Types.Path() != home {
			return refactor.Refusef("%q is used from package %s (at %s); renaming it to the unexported name %q would break that",
				r.old, rf.pkg.Types.Path(), r.ws.Position(rf.id.Pos()), r.new)
		}
	}
	return nil
}

// checkLexical applies Go's scoping rules to symbols that live in a scope
// (everything except fields and methods).
func (r *renamer) checkLexical() error {
	for _, t := range r.targets {
		decl := t.Parent()
		if decl == nil {
			continue // field or method
		}
		// 1. Clash in the declaring scope.
		if o := decl.Lookup(r.new); o != nil {
			return refactor.Refusef("%q is already declared in the same scope, at %s", r.new, r.ws.Position(o.Pos()))
		}
		// Package-level names also clash with imports in any file.
		if decl == t.Pkg().Scope() {
			for i := 0; i < decl.NumChildren(); i++ {
				if o := decl.Child(i).Lookup(r.new); o != nil {
					return refactor.Refusef("%q would clash with the import %q at %s", r.new, r.new, r.ws.Position(o.Pos()))
				}
			}
		}
		// 2. Shadowing: a reference sits in an inner scope that already
		// declares the new name, so it would bind to that instead.
		for _, rf := range r.refs {
			if rf.pkg.Types != t.Pkg() || rf.id.Pos() == t.Pos() {
				continue
			}
			s := t.Pkg().Scope().Innermost(rf.id.Pos())
			if s == nil {
				continue
			}
			if s2, o := s.LookupParent(r.new, rf.id.Pos()); o != nil && strictlyInside(s2, decl) {
				return refactor.Refusef("after renaming, the reference at %s would refer to the %s %q declared at %s instead (choose another name, or rename that one first)",
					r.ws.Position(rf.id.Pos()), kind(o), r.new, r.ws.Position(o.Pos()))
			}
		}
		// 3. Capture: an existing use of the new name, inside our scope,
		// refers to an outer declaration that our renamed symbol would hide.
		for _, p := range r.ws.Pkgs {
			if p.Types != t.Pkg() || p.TypesInfo == nil {
				continue
			}
			for id, o := range p.TypesInfo.Uses {
				if id.Name != r.new || o == nil || o.Parent() == nil {
					continue
				}
				if decl == t.Pkg().Scope() {
					// any file in the package
				} else if !decl.Contains(id.Pos()) || id.Pos() < t.Pos() {
					continue
				}
				if strictlyInside(decl, o.Parent()) {
					return refactor.Refusef("renaming to %q would hide the %s %q declared at %s, which is used at %s",
						r.new, kind(o), r.new, declaredAt(r.ws, o), r.ws.Position(id.Pos()))
				}
			}
		}
	}
	return nil
}

// strictlyInside reports whether inner is a proper descendant of outer.
func strictlyInside(inner, outer *types.Scope) bool {
	for s := inner.Parent(); s != nil; s = s.Parent() {
		if s == outer {
			return true
		}
	}
	return false
}

// checkFieldOrMethod checks clashes for fields and methods, which are not
// scoped lexically but looked up through types (including promotion via
// embedding).
func (r *renamer) checkFieldOrMethod() error {
	obj := r.first
	if obj.Parent() != nil {
		return nil
	}
	ws := r.ws

	// Every selector that reaches our symbol must not also reach an
	// existing member with the new name at the same or shallower depth.
	for _, p := range ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, sel := range p.TypesInfo.Selections {
			switch {
			case keyOf(ws.Fset, sel.Obj()) == r.key:
				if o, idx, _ := types.LookupFieldOrMethod(sel.Recv(), true, sel.Obj().Pkg(), r.new); o != nil && len(idx) <= len(sel.Index()) {
					return refactor.Refusef("%s already has a %s named %q (declared at %s)",
						typeString(sel.Recv()), kind(o), r.new, declaredAt(ws, o))
				}
			case sel.Obj().Name() == r.new:
				// The reverse: an existing selection of the new name would be
				// captured by our symbol if ours is shallower.
				if o, idx, _ := types.LookupFieldOrMethod(sel.Recv(), true, obj.Pkg(), r.old); o != nil &&
					keyOf(ws.Fset, o) == r.key && len(idx) < len(sel.Index()) {
					return refactor.Refusef("renaming would hide the promoted %s %q of %s that is used at %s",
						kind(sel.Obj()), r.new, typeString(sel.Recv()), declaredAt(ws, sel.Obj()))
				}
			}
		}
	}

	switch o := obj.(type) {
	case *types.Var: // field
		st, named := r.enclosingStruct()
		if st != nil {
			for i := 0; i < st.NumFields(); i++ {
				if f := st.Field(i); f.Name() == r.new {
					return refactor.Refusef("the struct already has a field named %q (at %s)", r.new, ws.Position(f.Pos()))
				}
			}
		}
		if named != nil {
			if m, _, _ := types.LookupFieldOrMethod(types.NewPointer(named), false, obj.Pkg(), r.new); m != nil {
				return refactor.Refusef("type %s already has a %s %q (at %s)", named.Obj().Name(), kind(m), r.new, declaredAt(ws, m))
			}
		}
	case *types.Func: // method
		named := receiverNamed(o)
		if named == nil {
			return nil
		}
		if m, _, _ := types.LookupFieldOrMethod(types.NewPointer(named), false, obj.Pkg(), r.new); m != nil {
			return refactor.Refusef("type %s already has a %s %q (at %s)", named.Obj().Name(), kind(m), r.new, declaredAt(ws, m))
		}
		return r.checkInterfaces(named)
	}
	return nil
}

// dynamicMethods are method names that are commonly called through
// interfaces the type checker cannot see at the call site (fmt.Println
// takes `any` but calls String; encoding/json calls MarshalJSON, ...).
var dynamicMethods = map[string]string{
	"String": "fmt.Stringer", "GoString": "fmt.GoStringer", "Format": "fmt.Formatter", "Error": "error",
	"Unwrap": "errors.Unwrap", "Is": "errors.Is", "As": "errors.As",
	"MarshalJSON": "json.Marshaler", "UnmarshalJSON": "json.Unmarshaler",
	"MarshalText": "encoding.TextMarshaler", "UnmarshalText": "encoding.TextUnmarshaler",
	"MarshalBinary": "encoding.BinaryMarshaler", "UnmarshalBinary": "encoding.BinaryUnmarshaler",
	"MarshalYAML": "yaml.Marshaler", "UnmarshalYAML": "yaml.Unmarshaler",
	"Scan": "sql.Scanner", "Value": "driver.Valuer",
}

// checkInterfaces refuses to rename a method that makes some type in the
// workspace satisfy an interface mentioning the method's name.
func (r *renamer) checkInterfaces(recv *types.Named) error {
	if iface, ok := dynamicMethods[r.old]; ok {
		return refactor.Refusef("method %s.%s probably implements %s, which is called dynamically; renaming it could silently change behavior",
			recv.Obj().Name(), r.old, iface)
	}
	ifaces := r.interfacesWithMethod(r.old)
	if len(ifaces) == 0 {
		return nil
	}
	// Any workspace type whose method set reaches our method (directly or
	// through embedding) must not be relying on it for an interface.
	// Candidates are checked in source order so messages are deterministic.
	var candidates []*types.Named
	seen := map[string]bool{}
	for _, p := range r.ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, o := range p.TypesInfo.Defs {
			tn, ok := o.(*types.TypeName)
			if !ok || tn.IsAlias() {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok || seen[keyOf(r.ws.Fset, tn)] {
				continue
			}
			m, _, _ := types.LookupFieldOrMethod(types.NewPointer(named), false, r.first.Pkg(), r.old)
			if m == nil || keyOf(r.ws.Fset, m) != r.key {
				continue
			}
			seen[keyOf(r.ws.Fset, tn)] = true
			candidates = append(candidates, named)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return keyOf(r.ws.Fset, candidates[i].Obj()) < keyOf(r.ws.Fset, candidates[j].Obj())
	})
	for _, named := range candidates {
		if named.TypeParams().Len() > 0 {
			return refactor.Refusef("method %s is on generic type %s and an interface with a method %q exists (%s); cannot prove renaming is safe",
				r.old, named.Obj().Name(), r.old, ifaces[0].name)
		}
		for _, it := range ifaces {
			if types.Implements(named, it.iface) || types.Implements(types.NewPointer(named), it.iface) {
				return refactor.Refusef("type %s implements interface %s through method %s; renaming the method would break that (rename the interface method instead, which is not supported yet)",
					named.Obj().Name(), it.name, r.old)
			}
		}
	}
	return nil
}

type namedIface struct {
	name  string
	iface *types.Interface
	named bool // name is a declared type name, not the interface literal
}

// interfacesWithMethod collects every interface type reachable from the
// workspace's types (named interfaces, interface parameters of called
// functions, anonymous interfaces) that has a method called name. Named
// interfaces are listed first and an interface reached both through its
// name and directly is reported by its name, so messages are stable
// regardless of map iteration order.
func (r *renamer) interfacesWithMethod(name string) []namedIface {
	var out []namedIface
	index := map[*types.Interface]int{}
	record := func(it *types.Interface, label string, named bool) {
		hasMethod := false
		for i := 0; i < it.NumMethods(); i++ {
			if it.Method(i).Name() == name {
				hasMethod = true
				break
			}
		}
		if !hasMethod {
			return
		}
		if i, ok := index[it]; ok {
			if named && !out[i].named {
				out[i].name, out[i].named = label, true
			}
			return
		}
		index[it] = len(out)
		out = append(out, namedIface{label, it, named})
	}
	seen := map[types.Type]bool{}
	var visit func(t types.Type)
	visit = func(t types.Type) {
		if t == nil || seen[t] {
			return
		}
		seen[t] = true
		switch t := t.(type) {
		case *types.Alias:
			visit(types.Unalias(t))
		case *types.Named:
			if it, ok := t.Underlying().(*types.Interface); ok {
				record(it, t.String(), true)
			}
			visit(t.Underlying())
		case *types.Interface:
			record(t, t.String(), false)
		case *types.Pointer:
			visit(t.Elem())
		case *types.Slice:
			visit(t.Elem())
		case *types.Array:
			visit(t.Elem())
		case *types.Chan:
			visit(t.Elem())
		case *types.Map:
			visit(t.Key())
			visit(t.Elem())
		case *types.Signature:
			for _, tup := range []*types.Tuple{t.Params(), t.Results()} {
				for i := 0; i < tup.Len(); i++ {
					visit(tup.At(i).Type())
				}
			}
		case *types.Struct:
			for i := 0; i < t.NumFields(); i++ {
				visit(t.Field(i).Type())
			}
		}
	}
	for _, p := range r.ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, tv := range p.TypesInfo.Types {
			visit(tv.Type)
		}
		for _, o := range p.TypesInfo.Defs {
			if o != nil {
				visit(o.Type())
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].named != out[j].named {
			return out[i].named
		}
		return out[i].name < out[j].name
	})
	return out
}

// enclosingStruct finds the struct type (and its named type, if any) that
// declares the field r.first.
func (r *renamer) enclosingStruct() (*types.Struct, *types.Named) {
	pos := r.first.Pos()
	path := r.ws.Fset.Position(pos).Filename
	pkg, file := r.ws.FileSyntax(path)
	if file == nil {
		return nil, nil
	}
	nodes, _ := astutil.PathEnclosingInterval(file, pos, pos)
	for i, n := range nodes {
		stExpr, ok := n.(*ast.StructType)
		if !ok {
			continue
		}
		st, _ := pkg.TypesInfo.TypeOf(stExpr).(*types.Struct)
		var named *types.Named
		if i+1 < len(nodes) {
			if ts, ok := nodes[i+1].(*ast.TypeSpec); ok && ts.Type == stExpr {
				if tn, ok := pkg.TypesInfo.Defs[ts.Name].(*types.TypeName); ok {
					named, _ = tn.Type().(*types.Named)
				}
			}
		}
		return st, named
	}
	return nil, nil
}

func receiverNamed(f *types.Func) *types.Named {
	recv := f.Signature().Recv()
	if recv == nil {
		return nil
	}
	t := recv.Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := types.Unalias(t).(*types.Named); ok {
		return n.Origin()
	}
	return nil
}

// checkEmbeddedType refuses to rename a type that is embedded in a struct,
// because the embedded field's name (and every selector using it) would
// change too.
func (r *renamer) checkEmbeddedType() error {
	if _, ok := r.first.(*types.TypeName); !ok {
		return nil
	}
	for _, p := range r.ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, o := range p.TypesInfo.Defs {
			v, ok := o.(*types.Var)
			if !ok || !v.Embedded() {
				continue
			}
			t := v.Type()
			if ptr, ok := t.(*types.Pointer); ok {
				t = ptr.Elem()
			}
			var tn *types.TypeName
			switch t := t.(type) {
			case *types.Named:
				tn = t.Origin().Obj()
			case *types.Alias:
				tn = t.Obj()
			}
			if tn != nil && keyOf(r.ws.Fset, tn) == r.key {
				return refactor.Refusef("type %s is embedded in a struct at %s; renaming it would also rename that field, which is not supported yet",
					r.old, r.ws.Position(v.Pos()))
			}
		}
	}
	return nil
}

// docEdits renames the old name where it starts the doc comment of a
// renamed declaration (a function, method, type, variable, constant or
// field), as Go's convention has it. It returns the edits and the
// comments it changed.
func (r *renamer) docEdits() ([]edit.FileEdit, map[token.Pos]bool) {
	lead := regexp.MustCompile(`^//\s*` + regexp.QuoteMeta(r.old) + `\b`)
	var fes []edit.FileEdit
	done := map[token.Pos]bool{}
	seen := map[string]bool{}
	for _, t := range r.targets {
		pos := r.ws.Fset.Position(t.Pos())
		if !t.Pos().IsValid() || seen[pos.String()] || !r.ws.Contains(pos.Filename) {
			continue
		}
		seen[pos.String()] = true
		_, f := r.ws.FileSyntax(pos.Filename)
		if f == nil {
			continue
		}
		doc := docFor(f, t.Pos())
		if doc == nil || len(doc.List) == 0 || done[doc.List[0].Pos()] {
			continue
		}
		c := doc.List[0]
		loc := lead.FindStringIndex(c.Text)
		if loc == nil {
			continue
		}
		at := r.ws.Fset.Position(c.Pos()).Offset + loc[1] - len(r.old)
		done[c.Pos()] = true
		fes = append(fes, edit.FileEdit{Path: pos.Filename, Edits: []edit.TextEdit{{Start: at, End: at + len(r.old), NewText: r.new}}})
	}
	return fes, done
}

// docFor returns the doc comment of the declaration whose name is at pos:
// its spec's, or its declaration's when that declares only it.
func docFor(f *ast.File, pos token.Pos) *ast.CommentGroup {
	var doc *ast.CommentGroup
	ast.Inspect(f, func(n ast.Node) bool {
		if doc != nil || n == nil || n.Pos() > pos || n.End() <= pos {
			return doc == nil && n != nil && n.Pos() <= pos && pos < n.End()
		}
		switch n := n.(type) {
		case *ast.FuncDecl:
			if n.Name.Pos() == pos {
				doc = n.Doc
			}
		case *ast.GenDecl:
			for _, sp := range n.Specs {
				var names []*ast.Ident
				var spDoc *ast.CommentGroup
				switch sp := sp.(type) {
				case *ast.TypeSpec:
					names, spDoc = []*ast.Ident{sp.Name}, sp.Doc
				case *ast.ValueSpec:
					names, spDoc = sp.Names, sp.Doc
				}
				for _, id := range names {
					if id.Pos() != pos {
						continue
					}
					doc = spDoc
					if doc == nil && len(n.Specs) == 1 && len(names) == 1 {
						doc = n.Doc
					}
				}
			}
		case *ast.Field:
			if len(n.Names) == 1 && n.Names[0].Pos() == pos {
				doc = n.Doc
			}
		}
		return doc == nil
	})
	return doc
}

var wordRE = func(w string) *regexp.Regexp { return regexp.MustCompile(`\b` + regexp.QuoteMeta(w) + `\b`) }

func (r *renamer) plan() (*refactor.Plan, error) {
	ws := r.ws
	var fes []edit.FileEdit
	files := map[string]bool{}
	add := func(pos token.Pos) error {
		p := ws.Fset.Position(pos)
		if !ws.Contains(p.Filename) {
			return refactor.Refusef("a reference to %q is in %s, outside the workspace", r.old, p.Filename)
		}
		files[p.Filename] = true
		fes = append(fes, edit.FileEdit{Path: p.Filename, Edits: []edit.TextEdit{{
			Start: p.Offset, End: p.Offset + len(r.old), NewText: r.new,
		}}})
		return nil
	}
	for _, rf := range r.refs {
		if err := add(rf.id.Pos()); err != nil {
			return nil, err
		}
	}
	// Type-switch variables are declared by an identifier that has no
	// object of its own; cover it via the implicit objects' position.
	for _, t := range r.targets {
		if t.Pos().IsValid() && ws.Contains(ws.Fset.Position(t.Pos()).Filename) {
			if err := add(t.Pos()); err != nil {
				return nil, err
			}
		}
	}
	docFes, renamedDocs := r.docEdits()
	fes = edit.Merge(fes)
	n := 0
	for _, fe := range fes {
		n += len(fe.Edits)
	}
	fes = edit.Merge(append(fes, docFes...))

	plan := &refactor.Plan{
		Summary: fmt.Sprintf("renamed %s %q to %q (%d occurrence%s in %d file%s)",
			kind(r.first), r.old, r.new, n, textutil.Plural(n), len(fes), textutil.Plural(len(fes))),
		Edits: fes,
	}
	if token.IsExported(r.old) && token.IsExported(r.new) && (isPackageLevel(r.first) || r.first.Parent() == nil) {
		plan.Notes = append(plan.Notes, fmt.Sprintf("%q is exported: code outside this workspace that uses it was not updated", r.old))
	}
	if len(docFes) > 0 {
		plan.Notes = append(plan.Notes, fmt.Sprintf("renamed %q in the doc comment that starts with it", r.old))
	}
	if mentions := r.commentMentions(files, renamedDocs); len(mentions) > 0 {
		plan.Notes = append(plan.Notes, fmt.Sprintf("comments still mention %q at %s; update them if they refer to this symbol",
			r.old, strings.Join(mentions, ", ")))
	}
	return plan, nil
}

// commentMentions lists (up to 5) comment positions in the edited files
// that mention the old name as a word. Comments are not code, so they are
// left alone, but the model may want to fix them.
func (r *renamer) commentMentions(files map[string]bool, skip map[token.Pos]bool) []string {
	re := wordRE(r.old)
	var out []string
	seen := map[string]bool{}
	// A local's comments are those in its scope; elsewhere the name is
	// someone else's.
	from, to := token.NoPos, token.NoPos
	if sc := r.first.Parent(); sc != nil && sc != types.Universe && r.first.Pkg() != nil && sc != r.first.Pkg().Scope() {
		from, to = sc.Pos(), sc.End()
	}
	for path := range files {
		_, f := r.ws.FileSyntax(path)
		if f == nil {
			continue
		}
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if skip[c.Pos()] || from.IsValid() && (c.Pos() < from || c.End() > to) {
					continue
				}
				if loc := re.FindStringIndex(c.Text); loc != nil {
					s := r.ws.Position(c.Pos() + token.Pos(loc[0]))
					if !seen[s] {
						seen[s] = true
						out = append(out, s)
					}
				}
			}
		}
	}
	sort.Strings(out)
	if len(out) > 5 {
		out = append(out[:5], fmt.Sprintf("and %d more", len(out)-5))
	}
	return out
}

func isPackageLevel(obj types.Object) bool {
	return obj.Pkg() != nil && obj.Parent() == obj.Pkg().Scope()
}

func kind(obj types.Object) string {
	switch o := obj.(type) {
	case *types.Var:
		if o.IsField() {
			return "field"
		}
		if isPackageLevel(o) {
			return "package-level variable"
		}
		return "variable"
	case *types.Const:
		return "constant"
	case *types.TypeName:
		if _, ok := o.Type().(*types.TypeParam); ok {
			return "type parameter"
		}
		return "type"
	case *types.Func:
		if o.Signature().Recv() != nil {
			return "method"
		}
		return "function"
	case *types.PkgName:
		return "import"
	case *types.Builtin:
		return "builtin"
	}
	return "symbol"
}

func declaredAt(ws *workspace.Workspace, o types.Object) string {
	if o.Pkg() == nil {
		return "universe scope (predeclared)"
	}
	return ws.Position(o.Pos())
}

func typeString(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string { return p.Name() })
}
