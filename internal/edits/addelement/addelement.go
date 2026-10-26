// Package addelement implements add_element, an edit: it may change
// behavior.
package addelement

import (
	"encoding/json"
	"fmt"
	"go/ast"
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
Identify the literal by file and the line it starts on (its type, or { for an elided type); when more than one starts there, give type as written ("[]pair"; &Plan is written "Plan"). element is "Key: value" or a value; packages it names are imported.`
}

func (*AddElement) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":    {"type": "string",  "description": "Path of the file, relative to the workspace root."},
    "line":    {"type": "integer", "description": "1-based line the composite literal starts on."},
    "type":    {"type": "string",  "description": "Optional: the literal's type as written, when more than one literal starts on the line."},
    "element": {"type": "string",  "description": "The element: \"Key: value\" or a value."},
    "after":   {"type": "string",  "description": "Optional: put it after the element with this key."},
    "before":  {"type": "string",  "description": "Optional: put it before the element with this key."},
    "imports": {"type": "array", "items": {"type": "string"}, "description": "Optional: import paths to use for package names the element uses, when the name alone is ambiguous."}
  },
  "required": ["file", "line", "element"],
  "additionalProperties": false
}`)
}

func (*AddElement) ChangesBehavior() {}

// Plan checks preconditions and computes the edits.
func (*AddElement) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File    string   `json:"file"`
		Line    int      `json:"line"`
		Type    string   `json:"type,omitempty"`
		Element string   `json:"element"`
		After   string   `json:"after,omitempty"`
		Before  string   `json:"before,omitempty"`
		Imports []string `json:"imports,omitempty"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.After != "" && a.Before != "" {
		return nil, refactor.Refusef("give after or before, not both")
	}
	path := ws.Abs(a.File)
	opened, err := site.At(ws, path)
	if err != nil {
		return nil, err
	}
	pkg, file, src := opened.Pkg, opened.File, opened.Src
	tf := opened.TF

	var lits []*ast.CompositeLit
	ast.Inspect(file, func(n ast.Node) bool {
		if lit, ok := n.(*ast.CompositeLit); ok && tf.Line(lit.Pos()) == a.Line && (a.Type == "" || typeOf(lit) == a.Type) {
			lits = append(lits, lit)
		}
		return true
	})
	switch {
	case len(lits) == 0 && a.Type != "":
		return nil, refactor.Refusef("no composite literal of type %s starts on line %d of %s", a.Type, a.Line, a.File)
	case len(lits) == 0:
		return nil, refactor.Refusef("no composite literal starts on line %d of %s", a.Line, a.File)
	case len(lits) > 1:
		var types []string
		for _, l := range lits {
			types = append(types, typeOf(l))
		}
		return nil, refactor.Refusef("%d composite literals start on line %d of %s (%s); give type", len(lits), a.Line, a.File, strings.Join(types, ", "))
	}
	lit := lits[0]

	code, err := gocode.ParseStatements("_ = []any{" + a.Element + "}")
	if err != nil {
		return nil, refactor.Refusef("element must be \"Key: value\" or a value: %v", err)
	}
	if n := len(code.Body.List[0].(*ast.AssignStmt).Rhs[0].(*ast.CompositeLit).Elts); n != 1 {
		return nil, refactor.Refusef("element must be one element, not %d", n)
	}

	at, text, err := place(tf.Offset, src, lit, a.After, a.Before, strings.TrimSpace(a.Element))
	if err != nil {
		return nil, err
	}
	adder := imports.New(ws, pkg, file, src)
	added, err := gocode.AddImports(ws, pkg, file, adder, lit.Lbrace, code, a.Imports)
	if err != nil {
		return nil, err
	}
	plan := &refactor.Plan{
		Summary:  fmt.Sprintf("added an element to the %s literal on line %d of %s", typeOf(lit), a.Line, a.File),
		Edits:    []edit.FileEdit{{Path: path, Edits: append([]edit.TextEdit{{Start: at, End: at, NewText: text}}, adder.Edits()...)}},
		Supplied: a.Element,
	}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("added", added))
	}
	return plan, nil
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
func place(offset func(p token.Pos) int, src []byte, lit *ast.CompositeLit, after, before, elem string) (int, string, error) {
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
		return at, indent + elem + ",\n", nil
	case multiline && len(lit.Elts) == 0:
		at := lineStart(src, rbrace)
		return at, gocode.IndentOf(src, at) + "\t" + elem + ",\n", nil
	case len(lit.Elts) == 0:
		return lbrace + 1, elem, nil
	case i < len(lit.Elts):
		return offset(lit.Elts[i].Pos()), elem + ", ", nil
	default:
		return offset(lit.Elts[i-1].End()), ", " + elem, nil
	}
}

// lineStart is the offset of the start of the line containing off.
func lineStart(src []byte, off int) int {
	for off > 0 && src[off-1] != '\n' {
		off--
	}
	return off
}
