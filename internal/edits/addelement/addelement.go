// Package addelement implements add_element, an edit: it may change
// behavior.
package addelement

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// AddElement is the "add_element" edit.
type AddElement struct{}

// New returns the add_element edit.
func New() *AddElement { return &AddElement{} }

func (*AddElement) Name() string { return "add_element" }

func (*AddElement) Description() string {
	return `Add an element to a composite literal: a field to a struct literal, an entry to a map literal, a value to a slice or array literal; an edit. At the line of var codes = map[string]int{, element "sms": 3 adds that entry at the end; after or before puts it next to the element with that key (a field name, or a map key as written).
Identify the literal by file and the line it starts on (its type, or { for an elided type; when more than one starts there, give type as written: "[]pair"; &Plan is written "Plan"), or by file and name, the package-level variable it initializes. element is "Key: value" or a value; packages it names are imported. A key the literal has already is merged when both values are keyed literals: {Sun: 6} and {Soil: "loam"} give {Sun: 6, Soil: "loam"}.
from adds every element of another variable's literal, written by element as a template ($key, $value; copied without one): name "beds", from "soils", element "$key: {Soil: $value}". from the literal itself rewrites each element.`
}

func (*AddElement) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":    {"type": "string",  "description": "Path of the file, relative to the workspace root."},
    "line":    {"type": "integer", "description": "1-based line the composite literal starts on; or give name."},
    "name":    {"type": "string",  "description": "The package-level variable the literal initializes; or give line."},
    "type":    {"type": "string",  "description": "Optional: the literal's type as written, when more than one literal starts on the line."},
    "element": {"type": "string",  "description": "The element: \"Key: value\" or a value; with from, a template of $key and $value."},
    "from":    {"type": "string",  "description": "Optional: a package-level variable whose literal's elements to add."},
    "after":   {"type": "string",  "description": "Optional: put it after the element with this key."},
    "before":  {"type": "string",  "description": "Optional: put it before the element with this key."},
    "imports": {"type": "array", "items": {"type": "string"}, "description": "Optional: import paths to use for package names the element uses, when the name alone is ambiguous."}
  },
  "required": ["file"],
  "additionalProperties": false
}`)
}

func (*AddElement) ChangesBehavior() {}

// Plan checks preconditions and computes the edits.
func (*AddElement) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File    string   `json:"file"`
		Line    int      `json:"line,omitempty"`
		Name    string   `json:"name,omitempty"`
		Type    string   `json:"type,omitempty"`
		Element string   `json:"element,omitempty"`
		From    string   `json:"from,omitempty"`
		After   string   `json:"after,omitempty"`
		Before  string   `json:"before,omitempty"`
		Imports []string `json:"imports,omitempty"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	switch {
	case a.After != "" && a.Before != "":
		return nil, refactor.Refusef("give after or before, not both")
	case (a.Line == 0) == (a.Name == ""):
		return nil, refactor.Refusef("give line, where the literal starts, or name, the package-level variable it initializes")
	case a.Element == "" && a.From == "":
		return nil, refactor.Refusef("give element, or from")
	}
	opened, err := site.At(ws, ws.Abs(a.File))
	if err != nil {
		return nil, err
	}
	s, lit := opened, (*ast.CompositeLit)(nil)
	if a.Name != "" {
		s, lit, err = literalOf(ws, opened, a.Name)
	} else {
		lit, err = literalAt(opened, a.Line, a.Type, a.File)
	}
	if err != nil {
		return nil, err
	}

	elems := []string{strings.TrimSpace(a.Element)}
	var from *ast.CompositeLit
	if a.From != "" {
		var fs *site.Site
		if fs, from, err = literalOf(ws, opened, a.From); err != nil {
			return nil, err
		}
		if elems, err = written(fs, from, a.From, strings.TrimSpace(a.Element)); err != nil {
			return nil, err
		}
	}
	code, err := gocode.ParseStatements("_ = []any{" + strings.Join(elems, ",\n") + "}")
	if err != nil {
		return nil, refactor.Refusef("element must be \"Key: value\" or a value: %v", err)
	}
	parsed := code.Body.List[0].(*ast.AssignStmt).Rhs[0].(*ast.CompositeLit).Elts
	if len(parsed) != len(elems) {
		return nil, refactor.Refusef("element must be one element, not %d", len(parsed)-len(elems)+1)
	}

	var edits []edit.TextEdit
	what := "an element"
	switch {
	case from == lit:
		edits, what = rewrite(s, lit, elems), fmt.Sprintf("the %d elements of %s, rewritten,", len(elems), a.From)
	default:
		if edits, err = add(s, lit, elems, parsed, a.After, a.Before); err != nil {
			return nil, err
		}
		if from != nil {
			what = fmt.Sprintf("the %d elements of %s", len(elems), a.From)
		}
	}
	adder := imports.New(ws, s.Pkg, s.File, s.Src)
	added, err := gocode.AddImports(ws, s.Pkg, s.File, adder, lit.Lbrace, code, a.Imports)
	if err != nil {
		return nil, err
	}
	path := s.TF.Name()
	plan := &refactor.Plan{
		Summary:  fmt.Sprintf("added %s to the %s literal on line %d of %s", what, typeOf(lit), s.TF.Line(lit.Pos()), ws.Rel(path)),
		Edits:    []edit.FileEdit{{Path: path, Edits: append(edits, adder.Edits()...)}},
		Supplied: a.Element,
	}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("added", added))
	}
	return plan, nil
}

// literalAt returns the composite literal that starts on line of s's
// file, of type typ when that is not empty.
func literalAt(s *site.Site, line int, typ, file string) (*ast.CompositeLit, error) {
	var lits []*ast.CompositeLit
	ast.Inspect(s.File, func(n ast.Node) bool {
		if lit, ok := n.(*ast.CompositeLit); ok && s.TF.Line(lit.Pos()) == line && (typ == "" || typeOf(lit) == typ) {
			lits = append(lits, lit)
		}
		return true
	})
	switch {
	case len(lits) == 0 && typ != "":
		return nil, refactor.Refusef("no composite literal of type %s starts on line %d of %s", typ, line, file)
	case len(lits) == 0:
		return nil, refactor.Refusef("no composite literal starts on line %d of %s", line, file)
	case len(lits) > 1:
		var types []string
		for _, l := range lits {
			types = append(types, typeOf(l))
		}
		return nil, refactor.Refusef("%d composite literals start on line %d of %s (%s); give type", len(lits), line, file, strings.Join(types, ", "))
	}
	return lits[0], nil
}

// literalOf returns the composite literal that the package-level variable
// name of s's package is initialized with, and the site of its file.
func literalOf(ws *workspace.Workspace, s *site.Site, name string) (*site.Site, *ast.CompositeLit, error) {
	v, ok := s.Pkg.Types.Scope().Lookup(name).(*types.Var)
	if !ok {
		return nil, nil, refactor.Refusef("no package-level variable %s in package %s", name, s.Pkg.Types.Name())
	}
	d, err := site.Declaring(ws, v)
	if err != nil {
		return nil, nil, err
	}
	for _, decl := range d.File.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, n := range vs.Names {
				if n.Pos() != v.Pos() || i >= len(vs.Values) {
					continue
				}
				e := ast.Unparen(vs.Values[i])
				if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
					e = u.X
				}
				if lit, ok := e.(*ast.CompositeLit); ok {
					return d, lit, nil
				}
			}
		}
	}
	return nil, nil, refactor.Refusef("%s is not initialized with a composite literal", name)
}

// written returns the elements of from, the literal of the variable name,
// written by the template tmpl, in which $key stands for an element's key
// and $value for its value; an empty template copies them.
func written(s *site.Site, from *ast.CompositeLit, name, tmpl string) ([]string, error) {
	var elems []string
	for _, e := range from.Elts {
		key, value := "", s.Text(e)
		if kv, ok := e.(*ast.KeyValueExpr); ok {
			key, value = s.Text(kv.Key), s.Text(kv.Value)
		}
		t := tmpl
		switch {
		case t == "" && key != "":
			t = "$key: $value"
		case t == "":
			t = "$value"
		case key == "" && strings.Contains(t, "$key"):
			return nil, refactor.Refusef("the elements of %s have no keys; element may use $value only", name)
		}
		elems = append(elems, strings.NewReplacer("$key", key, "$value", value).Replace(t))
	}
	return elems, nil
}

// rewrite returns the edits that replace each element of lit with the
// element of elems at its index.
func rewrite(s *site.Site, lit *ast.CompositeLit, elems []string) []edit.TextEdit {
	var edits []edit.TextEdit
	for i, e := range lit.Elts {
		edits = append(edits, edit.TextEdit{Start: s.Off(e.Pos()), End: s.Off(e.End()), NewText: elems[i]})
	}
	return edits
}

// add returns the edits that add elems, parsed as parsed, to lit. An
// element whose key lit has already is merged into that element.
func add(s *site.Site, lit *ast.CompositeLit, elems []string, parsed []ast.Expr, after, before string) ([]edit.TextEdit, error) {
	existing := map[string]*ast.KeyValueExpr{}
	for _, e := range lit.Elts {
		if kv, ok := e.(*ast.KeyValueExpr); ok {
			existing[types.ExprString(kv.Key)] = kv
		}
	}
	var edits []edit.TextEdit
	var news []string
	for i, p := range parsed {
		var old *ast.KeyValueExpr
		if kv, ok := p.(*ast.KeyValueExpr); ok {
			old = existing[types.ExprString(kv.Key)]
		}
		if old == nil {
			news = append(news, elems[i])
			continue
		}
		e, err := merge(s, old, elems[i])
		if err != nil {
			return nil, err
		}
		if e.NewText != "" {
			edits = append(edits, e)
		}
	}
	if len(news) > 0 {
		at, text, err := place(s.Off, s.Src, lit, after, before, news)
		if err != nil {
			return nil, err
		}
		edits = append(edits, edit.TextEdit{Start: at, End: at, NewText: text})
	}
	return edits, nil
}

// merge returns the edit that adds the fields of elem's value to the
// value of old, an element with the same key: both values must be keyed
// literals with no field in common.
func merge(s *site.Site, old *ast.KeyValueExpr, elem string) (edit.TextEdit, error) {
	key := types.ExprString(old.Key)
	src := "[]any{" + elem + "}"
	x, err := parser.ParseExpr(src)
	if err != nil {
		return edit.TextEdit{}, refactor.Refusef("element must be \"Key: value\" or a value: %v", err)
	}
	nv, _ := ast.Unparen(x.(*ast.CompositeLit).Elts[0].(*ast.KeyValueExpr).Value).(*ast.CompositeLit)
	ov, _ := ast.Unparen(old.Value).(*ast.CompositeLit)
	if !keyed(nv) || !keyed(ov) {
		return edit.TextEdit{}, refactor.Refusef("%s is in the literal already; its values merge only when both are keyed literals", key)
	}
	have := map[string]bool{}
	for _, e := range ov.Elts {
		have[types.ExprString(e.(*ast.KeyValueExpr).Key)] = true
	}
	var fields []string
	for _, e := range nv.Elts {
		f := types.ExprString(e.(*ast.KeyValueExpr).Key)
		if have[f] {
			return edit.TextEdit{}, refactor.Refusef("%s has %s already", key, f)
		}
		fields = append(fields, src[e.Pos()-1:e.End()-1])
	}
	if len(fields) == 0 {
		return edit.TextEdit{}, nil
	}
	at, text, err := place(s.Off, s.Src, ov, "", "", fields)
	return edit.TextEdit{Start: at, End: at, NewText: text}, err
}

// keyed reports whether l is a composite literal whose elements all have
// keys.
func keyed(l *ast.CompositeLit) bool {
	if l == nil {
		return false
	}
	for _, e := range l.Elts {
		if _, ok := e.(*ast.KeyValueExpr); !ok {
			return false
		}
	}
	return true
}

// typeOf is the literal's type as written, or "{}" when it is elided.
func typeOf(lit *ast.CompositeLit) string {
	if lit.Type == nil {
		return "{}"
	}
	return types.ExprString(lit.Type)
}

// place returns where the element goes and the text to insert there. A
// literal written on several lines gets the element on its own line; one
// written on one line gets it inline.
func place(offset func(p token.Pos) int, src []byte, lit *ast.CompositeLit, after, before string, elems []string) (int, string, error) {
	key := after + before
	i := len(lit.Elts) // the index the element goes before
	if key != "" {
		i = -1
		for j, e := range lit.Elts {
			if kv, ok := e.(*ast.KeyValueExpr); ok && types.ExprString(kv.Key) == key {
				i = j
				break
			}
		}
		if i < 0 {
			return 0, "", refactor.Refusef("no element with key %s in the literal", key)
		}
		if after != "" {
			i++
		}
	}
	lbrace, rbrace := offset(lit.Lbrace), offset(lit.Rbrace)
	multiline := strings.Contains(string(src[lbrace:rbrace]), "\n")
	switch {
	case multiline && len(lit.Elts) > 0 && strings.Contains(string(src[lbrace:offset(lit.Elts[0].Pos())]), "\n"):
		// One element per line: insert a whole line.
		var at int
		if i < len(lit.Elts) {
			at = lineStart(src, offset(lit.Elts[i].Pos()))
		} else {
			at = lineStart(src, rbrace)
		}
		indent := gocode.IndentOf(src, lineStart(src, offset(lit.Elts[0].Pos())))
		return at, indent + strings.Join(elems, ",\n"+indent) + ",\n", nil
	case multiline && len(lit.Elts) == 0:
		at := lineStart(src, rbrace)
		indent := gocode.IndentOf(src, at) + "\t"
		return at, indent + strings.Join(elems, ",\n"+indent) + ",\n", nil
	case len(lit.Elts) == 0:
		return lbrace + 1, strings.Join(elems, ", "), nil
	case i < len(lit.Elts):
		return offset(lit.Elts[i].Pos()), strings.Join(elems, ", ") + ", ", nil
	default:
		return offset(lit.Elts[i-1].End()), ", " + strings.Join(elems, ", "), nil
	}
}

// lineStart is the offset of the start of the line containing off.
func lineStart(src []byte, off int) int {
	for off > 0 && src[off-1] != '\n' {
		off--
	}
	return off
}
