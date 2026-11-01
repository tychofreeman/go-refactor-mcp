// Package replacetext implements replace_text, an edit: it may change
// behavior.
package replacetext

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
	"golang.org/x/tools/go/ast/astutil"
)

// ReplaceText is the "replace_text" edit.
type ReplaceText struct {
	refactor.Named[replaceTextArgs]
	// TextOnly refuses a change to a Go file outside its string
	// literals and comments.
	TextOnly bool
}

// New returns the replace_text edit.
func New() *ReplaceText { return &ReplaceText{Named: refactor.Called[replaceTextArgs]("replace_text")} }

func (*ReplaceText) Description() string {
	return `Replace exact text in a file: an edit, for what the other tools do not cover: text in string literals and comments, and files that are not Go (go.mod, JSON, test fixtures, docs). old "version 1.2" and new "version 1.3" change that text in README.md. To change Go code, replace_statements, replace_expression and insert_statements usually fit better.
old must be as in the file and occur once, or set all_occurrences; spacing within lines may differ (gofmt's alignment) where that still finds it. function limits the search to one function of a Go file. append with no old adds new at the end of the file. files in place of file makes the same replacement in several files (old must be in each). It addresses text, not lines, so it stays valid after earlier steps of a batch. In a Go file, packages new uses (strings.Join) are imported.`
}

// replaceTextArgs are the arguments of replace_text.
type replaceTextArgs struct {
	File           string   `json:"file,omitempty" desc:"Path of the file, relative to the workspace root: any file of the module, Go or not."`
	Files          []string `json:"files,omitempty" desc:"Instead of file: several files, to make the same replacement in each."`
	Old            string   `json:"old,omitempty" desc:"The text to replace, as it is in the file."`
	New            string   `json:"new" desc:"The text to put in its place."`
	AllOccurrences bool     `json:"all_occurrences,omitempty" desc:"Replace every occurrence of old in the file, not just the only one."`
	Function       string   `json:"function,omitempty" desc:"Optional: look for old only in this function of the Go file (Name, or Type.Method), its doc comment included."`
	Append         bool     `json:"append,omitempty" desc:"Instead of old: add new at the end of the file."`
	Imports        []string `json:"imports,omitempty" desc:"Optional: import paths to use for package names new uses, when the name alone is ambiguous."`
}

func (*ReplaceText) ChangesBehavior() {}

// Plan checks preconditions and computes the edits.
func (r *ReplaceText) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a replaceTextArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	switch {
	case a.File != "" && len(a.Files) > 0:
		return nil, refactor.Refusef("give file or files, not both")
	case a.File != "":
		a.Files = []string{a.File}
	}
	switch {
	case len(a.Files) == 0:
		return nil, refactor.Refusef("file (or files) is required")
	case a.Append && (a.Old != "" || a.Function != ""):
		return nil, refactor.Refusef("append adds new at the end; leave out old and function")
	case !a.Append && a.Old == "":
		return nil, refactor.Refusef("old is required, or append to add new at the end")
	case a.Function != "" && len(a.Files) > 1:
		return nil, refactor.Refusef("function names a function of one file; give file, not files")
	}
	plan := &refactor.Plan{Supplied: a.New}
	n := 0
	var added []string
	loose := false
	for _, file := range a.Files {
		fe, k, l, err := fileEdit(ws, file, a.Old, a.New, a.AllOccurrences, a.Function, a.Append)
		if err != nil {
			return nil, err
		}
		loose = loose || l
		if r.TextOnly {
			if err := codeChanged(ws, fe); err != nil {
				return nil, err
			}
		}
		paths, err := addImports(ws, &fe, a.Imports)
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			if !slices.Contains(added, p) {
				added = append(added, p)
			}
		}
		plan.Edits = append(plan.Edits, fe)
		n += k
	}
	if loose {
		plan.Notes = append(plan.Notes, "old matched with the spacing within lines ignored")
	}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("added", added))
	}
	where := ws.Rel(plan.Edits[0].Path)
	if len(a.Files) > 1 {
		where = fmt.Sprintf("%d files", len(a.Files))
	}
	plan.Summary = fmt.Sprintf("replaced %d occurrence%s of the text in %s", n, textutil.Plural(n), where)
	if a.Append {
		plan.Summary = "appended the text to " + where
	}
	return plan, nil
}

// fileEdit returns the edit replacing old with new in file: its one
// occurrence, or with all every occurrence; in function alone if one is
// named; or with appending, adding new at the end. loose says whether old
// matched only with the spacing within lines ignored.
func fileEdit(ws *workspace.Workspace, file, old, new string, all bool, function string, appending bool) (fe edit.FileEdit, n int, loose bool, err error) {
	path := ws.Abs(file)
	rel := ws.Rel(path)
	if !ws.Contains(path) {
		return fe, 0, false, refactor.Refusef("%s is outside the workspace %s", file, ws.Root)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return fe, 0, false, refactor.Refusef("cannot read %s: %v; to make a new file, use create_file", rel, err)
	}
	fe = edit.FileEdit{Path: path}
	if appending {
		if len(src) > 0 && src[len(src)-1] != '\n' {
			new = "\n" + new
		}
		fe.Edits = []edit.TextEdit{{Start: len(src), End: len(src), NewText: new}}
		return fe, 1, false, nil
	}
	lo, hi := 0, len(src)
	if function != "" {
		_, f := ws.FileSyntax(path)
		var fd *ast.FuncDecl
		if f != nil {
			fd = locate.Func(f, function)
		}
		if fd == nil {
			return fe, 0, false, refactor.Refusef("no function %s in %s", function, rel)
		}
		lo, hi = ws.Fset.Position(fd.Pos()).Offset, ws.Fset.Position(fd.End()).Offset
		if fd.Doc != nil {
			lo = ws.Fset.Position(fd.Doc.Pos()).Offset
		}
	}
	spans, loose, why := textutil.Locate(string(src), old, lo, hi, all, rel)
	if why != "" {
		return fe, 0, false, refactor.Refusef("%s", why)
	}
	for _, s := range spans {
		fe.Edits = append(fe.Edits, edit.TextEdit{Start: s[0], End: s[1], NewText: bracketed(ws, path, s[0], string(src[s[0]:s[1]]), new)})
	}
	return fe, len(spans), loose, nil
}

// codeChanged refuses fe, an edit of a file, when the file is Go and the
// edit changes its tokens other than string literals and comments: its
// code.
func codeChanged(ws *workspace.Workspace, fe edit.FileEdit) error {
	if !strings.HasSuffix(fe.Path, ".go") {
		return nil
	}
	src, err := os.ReadFile(fe.Path)
	if err != nil {
		return err
	}
	out, err := edit.Apply(src, fe.Edits)
	if err != nil {
		return err
	}
	before, after := codeTokens(src), codeTokens(out)
	for i := range max(len(before), len(after)) {
		if i < len(before) && i < len(after) && before[i].tok == after[i].tok && before[i].lit == after[i].lit {
			continue
		}
		line := 1
		if i < len(after) {
			line = after[i].line
		} else if len(after) > 0 {
			line = after[len(after)-1].line
		}
		return refactor.Refusef("replace_text changes Go only inside string literals and comments, and this changes code in %s at line %d. For code, use the edit that says what changes: replace_expression (an expression), add_element (an entry or field of a literal), add_case (a case or branch), insert_statements (new statements), add_field, add_declaration, or replace_statements; or a refactoring", ws.Rel(fe.Path), line)
	}
	return nil
}

// codeToken is a token of Go code: a string literal's text is left out.
type codeToken struct {
	tok  token.Token
	lit  string
	line int
}

// codeTokens returns the tokens of the Go source src, without comments,
// with string and character literals' text and how a semicolon was
// written left out.
func codeTokens(src []byte) []codeToken {
	var s scanner.Scanner
	file := token.NewFileSet().AddFile("", -1, len(src))
	s.Init(file, src, nil, 0)
	var toks []codeToken
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			return toks
		}
		if tok == token.STRING || tok == token.CHAR || tok == token.SEMICOLON {
			lit = ""
		}
		toks = append(toks, codeToken{tok, lit, file.Line(pos)})
	}
}

// bracketed returns new, to replace old at offset off of the Go file
// path, with each composite literal it would leave bare in an if, for or
// switch header put in parentheses, which Go requires there.
func bracketed(ws *workspace.Workspace, path string, off int, old, new string) string {
	_, file := ws.FileSyntax(path)
	e, err := parser.ParseExpr(new)
	if file == nil || err != nil {
		return new
	}
	tf := ws.Fset.File(file.Pos())
	nodes, _ := astutil.PathEnclosingInterval(file, tf.Pos(off), tf.Pos(off+len(old)))
	if !analysis.InControlHeader(e, append([]ast.Node{nil}, nodes...)) {
		return new
	}
	return gocode.BracketComposites(new)
}

// addImports adds to fe, an edit of a Go file, the imports of the
// packages its new text names that the file does not import, and returns
// their paths: a selector whose operand the file leaves unresolved.
func addImports(ws *workspace.Workspace, fe *edit.FileEdit, chosen []string) ([]string, error) {
	pkg, file := ws.FileSyntax(fe.Path)
	if file == nil || !strings.HasSuffix(fe.Path, ".go") {
		return nil, nil
	}
	src, err := os.ReadFile(fe.Path)
	if err != nil {
		return nil, err
	}
	after, err := edit.Apply(src, fe.Edits)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, fe.Path, after, 0)
	if err != nil {
		return nil, nil // the type check reports it
	}
	imported := map[string]bool{} // by name, as the edited file imports them
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		imported[gocode.ImportName(p)] = true
		if imp.Name != nil {
			imported[imp.Name.Name] = true
		}
	}
	var spans [][2]int // the new text's offsets in after
	delta := 0
	for _, e := range fe.Edits {
		start := e.Start + delta
		spans = append(spans, [2]int{start, start + len(e.NewText)})
		delta += len(e.NewText) - (e.End - e.Start)
	}
	var names []string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Obj != nil || imported[id.Name] || slices.Contains(names, id.Name) {
			return true
		}
		off := fset.Position(id.Pos()).Offset
		if slices.ContainsFunc(spans, func(s [2]int) bool { return s[0] <= off && off < s[1] }) {
			names = append(names, id.Name)
		}
		return true
	})
	if len(names) == 0 {
		return nil, nil
	}
	adder := imports.New(ws, pkg, file, src)
	pos := ws.Fset.File(file.Pos()).Pos(fe.Edits[0].Start)
	var added []string
	for _, name := range names {
		paths, err := gocode.ImportNames(ws, pkg, file, adder, pos, []string{name}, chosen)
		if errors.As(err, new(gocode.NoPackageError)) {
			continue // no package: a name the type check judges, such as one a later step declares
		}
		if err != nil {
			return nil, err
		}
		added = append(added, paths...)
	}
	fe.Edits = append(fe.Edits, adder.Edits()...)
	return added, nil
}
