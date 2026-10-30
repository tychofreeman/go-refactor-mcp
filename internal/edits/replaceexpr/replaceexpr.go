// Package replaceexpr implements replace_expression: an edit that
// replaces an expression (or every expression that means the same) with a
// new one.
package replaceexpr

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// ReplaceExpression is the "replace_expression" edit.
type ReplaceExpression struct{}

// New returns the replace_expression edit.
func New() *ReplaceExpression { return &ReplaceExpression{} }

func (*ReplaceExpression) Name() string { return "replace_expression" }

func (*ReplaceExpression) Description() string {
	return `Replace an expression with a new one: an edit, to change part of a statement without restating it. Replacing s.Unit with s.Attr("unit") on its line changes that read alone. With all_occurrences, every expression in the file that means the same is replaced: the same tokens, the same names referring to the same things, so c.Body in another method of the same type matches and d.Body of another type does not.
Identify the expression by file, the line it starts on and its source text (whitespace may differ; column if the text is on the line twice). new is parenthesized where its context needs it (b becomes (b + 1) in a * b), and packages it names are imported. A change in the expression's type is noted.`
}

func (*ReplaceExpression) InputSchema() json.RawMessage {
	return refactor.Schema[args]()
}

// ChangesBehavior marks this as an edit.
func (*ReplaceExpression) ChangesBehavior() {}

type args struct {
	File           string   `json:"file" desc:"Path of the file, relative to the workspace root."`
	Line           int      `json:"line" desc:"1-based line on which the expression starts."`
	Expression     string   `json:"expression" desc:"Source text of the expression to replace."`
	Column         int      `json:"column,omitempty" desc:"Optional 1-based column where it starts, if the text occurs more than once on the line."`
	New            string   `json:"new" desc:"The new expression, as Go source."`
	AllOccurrences bool     `json:"all_occurrences,omitempty" desc:"Also replace every other expression in the file that means the same."`
	Imports        []string `json:"imports,omitempty" desc:"Optional: import paths to use for package names the new expression uses, when the name alone is ambiguous."`
}

// Plan checks preconditions and computes the edits.
func (*ReplaceExpression) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	opened, err := site.At(ws, path)
	if err != nil {
		return nil, err
	}
	pkg, file, src := opened.Pkg, opened.File, opened.Src
	want, err := locate.ExprTokens(a.Expression)
	if err != nil {
		return nil, err
	}
	expr, err := locate.FindExpr(ws, file, src, want, a.Line, a.Column, a.AllOccurrences)
	if err != nil {
		return nil, err
	}
	newExpr, err := parser.ParseExpr(a.New)
	if err != nil {
		return nil, refactor.Refusef("new is not a Go expression: %v", err)
	}
	code, err := gocode.ParseStatements("_ = " + a.New)
	if err != nil {
		return nil, err
	}
	targets := []ast.Expr{expr}
	if a.AllOccurrences {
		targets = sameMeaning(ws.Fset, pkg.TypesInfo, file, src, expr)
	}
	adder := imports.New(ws, pkg, file, src)
	added, err := gocode.AddImports(ws, pkg, file, adder, expr.Pos(), code, a.Imports)
	if err != nil {
		return nil, err
	}
	removed := gocode.RemoveImports(pkg, file, adder, func(p token.Pos) bool {
		return slices.ContainsFunc(targets, func(t ast.Expr) bool { return t.Pos() <= p && p < t.End() })
	}, code)
	var es []edit.TextEdit
	for _, t := range targets {
		text := strings.TrimSpace(a.New)
		if path := analysis.PathTo(file, t); len(path) > 1 && analysis.NeedsParens(newExpr, path[1], t) {
			text = "(" + text + ")"
		} else if analysis.InControlHeader(newExpr, path) {
			text = gocode.BracketComposites(text)
		}
		off := ws.Fset.Position(t.Pos()).Offset
		es = append(es, edit.TextEdit{Start: off, End: ws.Fset.Position(t.End()).Offset, NewText: text})
	}
	n := len(targets)
	plan := &refactor.Plan{
		Summary: fmt.Sprintf("replaced %d occurrence%s of %s with %s in %s", n, textutil.Plural(n), strings.TrimSpace(a.Expression), strings.TrimSpace(a.New), a.File),
		Edits:   []edit.FileEdit{{Path: path, Edits: append(es, adder.Edits()...)}},
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	if err := types.CheckExpr(ws.Fset, pkg.Types, expr.Pos(), newExpr, info); err == nil {
		if from, to := pkg.TypesInfo.TypeOf(expr), info.TypeOf(newExpr); from != nil && to != nil && !types.Identical(types.Default(from), types.Default(to)) { // an untyped bool is a bool where it is used
			plan.Notes = append(plan.Notes, fmt.Sprintf("the expression's type changes from %s to %s", from, to))
		}
	}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("added", added))
	}
	if len(removed) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("removed", removed)+", which only the replaced expression used")
	}
	return plan, nil
}
