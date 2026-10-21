// Package fieldstomap implements the "fields_to_map" edit: move fields of
// a struct type into one map field, keyed by name, rewriting the
// literals and reads of the fields across the module.
package fieldstomap

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// FieldsToMap is the "fields_to_map" edit.
type FieldsToMap struct{}

// New returns the fields_to_map edit.
func New() *FieldsToMap { return &FieldsToMap{} }

func (*FieldsToMap) Name() string { return "fields_to_map" }

func (*FieldsToMap) Description() string {
	return `Move fields of a struct type into one map field, keyed by name: User{Email: e, Phone: p} becomes User{Contacts: map[string]string{"email": e, "sms": p}} and u.Email becomes u.Contacts["email"], in every package of the module. This is an edit, not a refactoring: a missing key reads as the zero value, as an unset field did, but reflection and equality see the map.
Use it to change a representation, such as fields per channel into a map keyed by channel, in one step.
Identify the struct by file and name (line to disambiguate); fields maps each field to move to its key; into is the map field, declared in place of the first field moved unless the struct has it already (with the same type).
Refuses fields of different types, a field that is assigned to, incremented or has its address taken (a write to a nil map panics; write through a method first), and a literal of the type without keys.`
}

func (*FieldsToMap) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":   {"type": "string",  "description": "Path of a file containing the struct type's declaration or a use, relative to the workspace root."},
    "name":   {"type": "string",  "description": "The struct type's name."},
    "line":   {"type": "integer", "description": "Optional 1-based line of that occurrence; without it, name is a package-level type of file's package."},
    "column": {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "fields": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Each field to move and its key in the map, e.g. {\"Email\": \"email\", \"Phone\": \"sms\"}."},
    "into":   {"type": "string",  "description": "Name of the map field, map[string]T for the fields' type T."}
  },
  "required": ["file", "name", "fields", "into"],
  "additionalProperties": false
}`)
}

// ChangesBehavior marks it as an edit.
func (*FieldsToMap) ChangesBehavior() {}

type args struct {
	File   string            `json:"file"`
	Name   string            `json:"name"`
	Line   int               `json:"line,omitempty"`
	Column int               `json:"column,omitempty"`
	Fields map[string]string `json:"fields"`
	Into   string            `json:"into"`
}

// move holds the state of one planning run.
type move struct {
	ws     *workspace.Workspace
	pkg    *packages.Package
	tn     *types.TypeName
	st     *ast.StructType
	path   string // the declaring file
	into   string
	keys   map[string]string         // key by field name
	at     map[token.Position]string // the moved fields' names, by declaration
	order  []string                  // the moved fields, in declaration order
	elem   types.Type
	exists bool // the struct declares into already

	srcs  map[string][]byte
	edits map[string][]edit.TextEdit // per file
	seen  map[string]bool            // nodes handled, by file and offset (packages have variants)
}

// Plan checks preconditions and computes the edits.
func (*FieldsToMap) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	switch {
	case !token.IsIdentifier(a.Into):
		return nil, refactor.Refusef("into %q is not a valid Go identifier", a.Into)
	case len(a.Fields) == 0:
		return nil, refactor.Refusef("fields is required: each field to move and its key")
	}
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	m := &move{ws: ws, pkg: pkg, into: a.Into, keys: a.Fields, at: map[token.Position]string{},
		srcs: map[string][]byte{}, edits: map[string][]edit.TextEdit{}, seen: map[string]bool{}}
	if err := m.declaration(id); err != nil {
		return nil, err
	}
	if err := m.fields(); err != nil {
		return nil, err
	}
	uses, err := m.rewriteUses()
	if err != nil {
		return nil, err
	}
	lits, err := m.rewriteLiterals()
	if err != nil {
		return nil, err
	}
	if err := m.declEdits(); err != nil {
		return nil, err
	}
	var fes []edit.FileEdit
	for path, es := range m.edits {
		fes = append(fes, edit.FileEdit{Path: path, Edits: es})
	}
	sort.Slice(fes, func(i, j int) bool { return fes[i].Path < fes[j].Path })
	return &refactor.Plan{
		Summary: fmt.Sprintf("moved %s of %s into %s %s (%d literal%s, %d use%s)",
			strings.Join(m.order, ", "), m.tn.Name(), m.into, types.TypeString(m.mapType(), types.RelativeTo(m.tn.Pkg())),
			lits, textutil.Plural(lits), uses, textutil.Plural(uses)),
		Edits: edit.Merge(fes),
	}, nil
}

// declaration finds the struct type's declaration.
func (m *move) declaration(id *ast.Ident) error {
	tn, ok := m.pkg.TypesInfo.ObjectOf(id).(*types.TypeName)
	if !ok || tn.Pkg() == nil || tn.Parent() != tn.Pkg().Scope() {
		return refactor.Refusef("%s at %s is not a package-level type", id.Name, m.ws.Position(id.Pos()))
	}
	m.tn = tn
	m.path = m.ws.Fset.Position(tn.Pos()).Filename
	for _, p := range m.ws.Pkgs {
		for _, f := range p.Syntax {
			if m.ws.Fset.File(f.Pos()).Name() != m.path {
				continue
			}
			for _, d := range f.Decls {
				g, ok := d.(*ast.GenDecl)
				if !ok || g.Tok != token.TYPE {
					continue
				}
				for _, s := range g.Specs {
					ts := s.(*ast.TypeSpec)
					if ts.Name.Name != tn.Name() || ts.Name.Pos() != tn.Pos() {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok || ts.Assign.IsValid() {
						return refactor.Refusef("%s is not declared as a struct type", tn.Name())
					}
					if ts.TypeParams != nil {
						return refactor.Refusef("%s has type parameters; this is not supported yet", tn.Name())
					}
					m.pkg, m.st = p, st
					return nil
				}
			}
		}
	}
	return refactor.Refusef("the declaration of %s is not in a loaded package", tn.Name())
}

// fields checks the fields to move and into, and finds the map's element
// type.
func (m *move) fields() error {
	st := m.tn.Type().Underlying().(*types.Struct)
	var first string
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if f.Name() == m.into {
			m.exists = true
		}
		if _, ok := m.keys[f.Name()]; !ok || f.Embedded() {
			continue
		}
		if m.elem == nil {
			m.elem, first = f.Type(), f.Name()
		} else if !types.Identical(m.elem, f.Type()) {
			q := types.RelativeTo(m.tn.Pkg())
			return refactor.Refusef("%s is %s %s and %s %s %s; the fields of one map must have one type",
				first, article(types.TypeString(m.elem, q)), types.TypeString(m.elem, q), f.Name(), article(types.TypeString(f.Type(), q)), types.TypeString(f.Type(), q))
		}
		m.order = append(m.order, f.Name())
		m.at[m.ws.Fset.Position(f.Pos())] = f.Name()
	}
	for name := range m.keys {
		if _, ok := m.at[m.position(name, st)]; !ok {
			return refactor.Refusef("%s has no field %s (embedded fields cannot be moved)", m.tn.Name(), name)
		}
	}
	if _, moving := m.keys[m.into]; moving {
		return refactor.Refusef("%s is one of the fields to move; into names the map they move to", m.into)
	}
	if m.exists {
		o, _, _ := types.LookupFieldOrMethod(m.tn.Type(), false, m.tn.Pkg(), m.into)
		if v, ok := o.(*types.Var); !ok || !types.Identical(v.Type(), m.mapType()) {
			return refactor.Refusef("%s already has %s, which is not a field of type %s", m.tn.Name(), m.into, types.TypeString(m.mapType(), types.RelativeTo(m.tn.Pkg())))
		}
	} else if o, _, _ := types.LookupFieldOrMethod(types.NewPointer(m.tn.Type()), false, m.tn.Pkg(), m.into); o != nil {
		return refactor.Refusef("%s already has a method or promoted field %s", m.tn.Name(), m.into)
	}
	return nil
}

func (m *move) position(name string, st *types.Struct) token.Position {
	for i := 0; i < st.NumFields(); i++ {
		if f := st.Field(i); f.Name() == name && !f.Embedded() {
			return m.ws.Fset.Position(f.Pos())
		}
	}
	return token.Position{}
}

func article(s string) string {
	if strings.ContainsAny(s[:1], "aeiou") {
		return "an"
	}
	return "a"
}

func (m *move) mapType() types.Type { return types.NewMap(types.Typ[types.String], m.elem) }

// moved returns the name of the moved field that obj is, or "".
func (m *move) moved(obj types.Object) string {
	if v, ok := obj.(*types.Var); !ok || !v.IsField() {
		return ""
	}
	return m.at[m.ws.Fset.Position(obj.Pos())]
}

// once reports whether n has not been handled yet, in any package variant.
func (m *move) once(n ast.Node) bool {
	k := m.ws.Fset.Position(n.Pos()).String()
	if m.seen[k] {
		return false
	}
	m.seen[k] = true
	return true
}

// rewriteUses rewrites every read of a moved field, x.F, to x.into["key"],
// and refuses writes.
func (m *move) rewriteUses() (int, error) {
	n := 0
	for _, p := range m.ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, f := range p.Syntax {
			var err error
			ast.Inspect(f, func(node ast.Node) bool {
				sel, ok := node.(*ast.SelectorExpr)
				if !ok || err != nil {
					return err == nil
				}
				s := p.TypesInfo.Selections[sel]
				if s == nil || s.Kind() != types.FieldVal {
					return true
				}
				name := m.moved(s.Obj())
				if name == "" || !m.once(sel) {
					return true
				}
				if how := written(analysis.PathTo(f, sel)); how != "" {
					err = refactor.Refusef("`%s` is %s at %s; a write to a map panics when the map is nil, so make the writes go through one method first (or write to the map there), then move the field",
						types.ExprString(sel), how, m.ws.Position(sel.Pos()))
					return false
				}
				path := m.ws.Fset.File(sel.Pos()).Name()
				m.edits[path] = append(m.edits[path], edit.TextEdit{Start: m.off(sel.Sel.Pos()), End: m.off(sel.Sel.End()),
					NewText: m.into + "[" + strconv.Quote(m.keys[name]) + "]"})
				n++
				return true
			})
			if err != nil {
				return 0, err
			}
		}
	}
	return n, nil
}

// written says how the selector at path[0] is written to, or "".
func written(path []ast.Node) string {
	cur := path[0]
	for _, n := range path[1:] {
		switch n := n.(type) {
		case *ast.ParenExpr:
			cur = n
			continue
		case *ast.AssignStmt:
			for _, l := range n.Lhs {
				if l == cur {
					return "assigned"
				}
			}
		case *ast.IncDecStmt:
			return "incremented or decremented"
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN && (n.Key == cur || n.Value == cur) {
				return "assigned by a range loop"
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				return "has its address taken"
			}
		}
		return ""
	}
	return ""
}

// lit is a composite literal of the struct type.
type lit struct {
	path string
	pkg  *packages.Package
	file *ast.File
	expr *ast.CompositeLit
}

// rewriteLiterals rewrites every literal of the type that sets a moved
// field, innermost first so that an outer literal carries the rewritten
// inner ones. It returns how many changed.
func (m *move) rewriteLiterals() (int, error) {
	var lits []lit
	for _, p := range m.ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, f := range p.Syntax {
			path := m.ws.Fset.File(f.Pos()).Name()
			ast.Inspect(f, func(n ast.Node) bool {
				cl, ok := n.(*ast.CompositeLit)
				if !ok || len(cl.Elts) == 0 {
					return true
				}
				if t, ok := p.TypesInfo.TypeOf(cl).(*types.Named); ok && m.isT(t) && m.once(cl) {
					lits = append(lits, lit{path: path, pkg: p, file: f, expr: cl})
				}
				return true
			})
		}
	}
	sort.Slice(lits, func(i, j int) bool {
		a, b := lits[i].expr, lits[j].expr
		return a.End()-a.Pos() < b.End()-b.Pos()
	})
	n := 0
	for _, l := range lits {
		changed, err := m.rewrite(l)
		if err != nil {
			return 0, err
		}
		if changed {
			n++
		}
	}
	return n, nil
}

func (m *move) isT(n *types.Named) bool {
	o := n.Obj()
	return o.Name() == m.tn.Name() && m.ws.Fset.Position(o.Pos()) == m.ws.Fset.Position(m.tn.Pos())
}

// rewrite rewrites one literal, if it sets a moved field: the first such
// element becomes into: map[string]T{...} with every moved one, and the
// others are removed.
func (m *move) rewrite(l lit) (bool, error) {
	cl := l.expr
	if _, keyed := cl.Elts[0].(*ast.KeyValueExpr); !keyed {
		return false, refactor.Refusef("the literal at %s lists the fields without keys; give it keys first", m.ws.Position(cl.Pos()))
	}
	src, err := m.source(l.path)
	if err != nil {
		return false, err
	}
	var entries []string
	first := -1
	var drop []int
	for i, e := range cl.Elts {
		kv := e.(*ast.KeyValueExpr)
		id, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		if id.Name == m.into {
			return false, refactor.Refusef("the literal at %s sets %s already; this is not supported yet", m.ws.Position(cl.Pos()), m.into)
		}
		key, ok := m.keys[id.Name]
		if !ok {
			continue
		}
		entries = append(entries, strconv.Quote(key)+": "+m.textWithEdits(l.path, src, kv.Value))
		if first < 0 {
			first = i
		} else {
			drop = append(drop, i)
		}
	}
	if first < 0 {
		return false, nil
	}
	mt, missing := analysis.TypeString(m.mapType(), l.file, l.pkg.Types, analysis.ImportMap(l.pkg.Types))
	if missing != "" {
		return false, refactor.Refusef("the literal at %s needs %s, but its file does not import %q", m.ws.Position(cl.Pos()), mt, missing)
	}
	kv := cl.Elts[first]
	m.replace(l.path, kv.Pos(), kv.End(), m.into+": "+mt+"{"+strings.Join(entries, ", ")+"}")
	for _, i := range drop {
		m.replace(l.path, cl.Elts[i-1].End(), cl.Elts[i].End(), "")
	}
	return true, nil
}

// textWithEdits returns n's source with the edits inside it applied, and
// drops those edits, which n's replacement now carries.
func (m *move) textWithEdits(path string, src []byte, n ast.Node) string {
	start, end := m.off(n.Pos()), m.off(n.End())
	var inner, rest []edit.TextEdit
	for _, e := range m.edits[path] {
		if start <= e.Start && e.End <= end {
			inner = append(inner, edit.TextEdit{Start: e.Start - start, End: e.End - start, NewText: e.NewText})
		} else {
			rest = append(rest, e)
		}
	}
	m.edits[path] = rest
	out, _ := edit.Apply(src[start:end], inner)
	return string(out)
}

func (m *move) replace(path string, from, to token.Pos, text string) {
	m.edits[path] = append(m.edits[path], edit.TextEdit{Start: m.off(from), End: m.off(to), NewText: text})
}

func (m *move) source(path string) ([]byte, error) {
	if s, ok := m.srcs[path]; ok {
		return s, nil
	}
	s, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m.srcs[path] = s
	return s, nil
}

// declEdits remove the moved fields from the struct, and declare into in
// place of the first unless it exists.
func (m *move) declEdits() error {
	src, err := m.source(m.path)
	if err != nil {
		return err
	}
	tf := m.ws.Fset.File(m.st.Pos())
	if tf.Line(m.st.Fields.Opening) == tf.Line(m.st.Fields.Closing) {
		return m.declOneLine(src)
	}
	declared := m.exists
	for _, f := range m.st.Fields.List {
		var in, out []string
		for _, n := range f.Names {
			if _, ok := m.keys[n.Name]; ok {
				in = append(in, n.Name)
			} else {
				out = append(out, n.Name)
			}
		}
		switch {
		case len(in) == 0:
			continue
		case len(out) > 0:
			return refactor.Refusef("%s is declared together with %s; move all of them or none", strings.Join(in, ", "), strings.Join(out, ", "))
		}
		start := f.Pos()
		if f.Doc != nil {
			start = f.Doc.Pos()
		}
		from := tf.LineStart(tf.Line(start))
		to := f.End()
		if f.Comment != nil {
			to = f.Comment.End()
		}
		if l := tf.Line(to); l < tf.LineCount() {
			to = tf.LineStart(l + 1)
		}
		text := ""
		if !declared {
			declared = true
			text = lineIndent(src, m.off(from)) + m.into + " " + m.mapString() + "\n"
		}
		m.replace(m.path, from, to, text)
	}
	return nil
}

// declOneLine rewrites a struct declared on one line, struct{ ID, Email
// string }, as fields on lines of their own.
func (m *move) declOneLine(src []byte) error {
	var lines []string
	declared := m.exists
	for _, f := range m.st.Fields.List {
		var keep []string
		moved := false
		for _, n := range f.Names {
			if _, ok := m.keys[n.Name]; ok {
				moved = true
			} else {
				keep = append(keep, n.Name)
			}
		}
		typ := string(src[m.off(f.Type.Pos()):m.off(f.Type.End())])
		switch {
		case len(f.Names) == 0:
			lines = append(lines, string(src[m.off(f.Pos()):m.off(f.End())]))
		case len(keep) > 0:
			lines = append(lines, strings.Join(keep, ", ")+" "+typ)
		}
		if moved && !declared {
			declared = true
			lines = append(lines, m.into+" "+m.mapString())
		}
	}
	m.replace(m.path, m.st.Fields.Opening, m.st.Fields.Closing+1, "{\n"+strings.Join(lines, "\n")+"\n}")
	return nil
}

// mapString writes the map type as the declaring file spells it.
func (m *move) mapString() string {
	for _, f := range m.pkg.Syntax {
		if m.ws.Fset.File(f.Pos()).Name() == m.path {
			s, _ := analysis.TypeString(m.mapType(), f, m.pkg.Types, analysis.ImportMap(m.pkg.Types))
			return s
		}
	}
	return types.TypeString(m.mapType(), types.RelativeTo(m.pkg.Types))
}

func (m *move) off(p token.Pos) int { return m.ws.Fset.Position(p).Offset }

func lineIndent(src []byte, start int) string {
	i := start
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	return string(src[start:i])
}
