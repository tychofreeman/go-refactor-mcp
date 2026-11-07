// Package extractsupertype implements extract_supertype for Go: some
// methods of a type named as a new interface.
package extractsupertype

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// ExtractSupertype is the "extract_supertype" refactoring for Go.
type ExtractSupertype struct{ refactor.Named[args] }

// New returns the extract_supertype refactoring.
func New() *ExtractSupertype {
	return &ExtractSupertype{Named: refactor.Called[args]("extract_supertype")}
}

func (*ExtractSupertype) Description() string {
	return `Name some methods of a type as a new interface, so that code can depend on just those. From a struct or another named type: with Water and Spray of Sprinkler, new_type Waterer declares type Waterer interface { Water(bed string, litres int) error; Spray() int } just before Sprinkler, and var _ Waterer = (*Sprinkler)(nil), which checks that Sprinkler implements it. From an interface, the methods move into the new one, which the old one embeds, so every use keeps working: Depth and Width of type Bed interface { Depth() int; Width() int; Name() string } into Size give type Bed interface { Size; Name() string }.
Identify the type by file and name; fields are the methods, new_type the interface's name.`
}

type args struct {
	File    string   `json:"file" desc:"Path of the file declaring the type, relative to the module root."`
	Name    string   `json:"name" desc:"The type."`
	Fields  []string `json:"fields" desc:"The methods to name in the new interface."`
	NewType string   `json:"new_type" desc:"Name of the new interface."`
}

// Plan checks preconditions and computes the edits.
func (*ExtractSupertype) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	st, err := site.At(ws, path)
	if err != nil {
		return nil, err
	}
	scope := st.Pkg.Types.Scope()
	switch {
	case len(a.Fields) == 0:
		return nil, refactor.Refusef("fields names the methods to put in the interface")
	case !token.IsIdentifier(a.NewType):
		return nil, refactor.Refusef("%q is not a name the interface can have", a.NewType)
	case scope.Lookup(a.NewType) != nil || types.Universe.Lookup(a.NewType) != nil:
		return nil, refactor.Refusef("%s is already declared; give new_type another name", a.NewType)
	}
	gen, spec := typeSpec(st.File, a.Name)
	tn, _ := scope.Lookup(a.Name).(*types.TypeName)
	if spec == nil || tn == nil {
		return nil, refactor.Refusef("%s declares no type %s", a.File, a.Name)
	}
	named, _ := tn.Type().(*types.Named)
	switch {
	case spec.TypeParams != nil:
		return nil, refactor.Refusef("%s is generic, which extract_supertype does not support yet", a.Name)
	case named == nil || tn.IsAlias():
		return nil, refactor.Refusef("%s is an alias; give the type it names", a.Name)
	}
	at := st.Off(gen.Pos())
	if gen.Doc != nil {
		at = st.Off(gen.Doc.Pos())
	}
	doc := fmt.Sprintf("// %s has %s of %s.\n", a.NewType, methodList(a.Fields), a.Name)
	if iface, ok := spec.Type.(*ast.InterfaceType); ok {
		return fromInterface(st, path, a, iface, at, doc)
	}
	mset := types.NewMethodSet(types.NewPointer(named))
	var lines []string
	for _, m := range a.Fields {
		sel := mset.Lookup(st.Pkg.Types, m)
		if sel == nil {
			return nil, refactor.Refusef("%s has no method %s", a.Name, m)
		}
		sig, err := signature(st, sel.Obj().Type().(*types.Signature))
		if err != nil {
			return nil, err
		}
		lines = append(lines, "\t"+m+sig)
	}
	text := fmt.Sprintf("%stype %s interface {\n%s\n}\n\nvar _ %s = (*%s)(nil)\n\n", doc, a.NewType, strings.Join(lines, "\n"), a.NewType, a.Name)
	return &refactor.Plan{
		Summary: fmt.Sprintf("declared %s, the interface of %s's methods %s", a.NewType, a.Name, strings.Join(a.Fields, ", ")),
		Edits:   []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: at, End: at, NewText: text}}}},
	}, nil
}

// fromInterface plans extract_supertype of an interface: the methods it
// declares move into the new interface, which it embeds in place of the
// first of them.
func fromInterface(st *site.Site, path string, a args, iface *ast.InterfaceType, at int, doc string) (*refactor.Plan, error) {
	var moved []*ast.Field
	for _, m := range a.Fields {
		var found *ast.Field
		for _, f := range iface.Methods.List {
			if len(f.Names) == 1 && f.Names[0].Name == m {
				found = f
			}
		}
		if found == nil {
			return nil, refactor.Refusef("%s does not declare %s; it embeds the interface that does, so extract from that one", a.Name, m)
		}
		moved = append(moved, found)
	}
	slices.SortFunc(moved, func(x, y *ast.Field) int { return int(x.Pos() - y.Pos()) })
	lines := map[int]int{}
	for _, f := range iface.Methods.List {
		lines[st.TF.Line(f.Pos())]++
	}
	var methods []string
	var edits []edit.TextEdit
	for i, f := range moved {
		if lines[st.TF.Line(f.Pos())] > 1 || st.TF.Line(f.Pos()) == st.TF.Line(iface.Pos()) {
			return nil, refactor.Refusef("%s shares its line with another method or the interface; put each method on its own line first", f.Names[0].Name)
		}
		start := f.Pos()
		if f.Doc != nil {
			start = f.Doc.Pos()
		}
		from, to := lineStart(st.Src, st.Off(start)), textutil.NextLine(st.Src, st.Off(f.End()))
		methods = append(methods, strings.TrimRight(string(st.Src[from:to]), "\n"))
		replace := ""
		if i == 0 {
			replace = gocode.IndentOf(st.Src, from) + a.NewType + "\n"
		}
		edits = append(edits, edit.TextEdit{Start: from, End: to, NewText: replace})
	}
	text := fmt.Sprintf("%stype %s interface {\n%s\n}\n\n", doc, a.NewType, strings.Join(methods, "\n"))
	edits = append([]edit.TextEdit{{Start: at, End: at, NewText: text}}, edits...)
	return &refactor.Plan{
		Summary: fmt.Sprintf("moved %s of %s into %s, which %s embeds", strings.Join(a.Fields, ", "), a.Name, a.NewType, a.Name),
		Edits:   []edit.FileEdit{{Path: path, Edits: edits}},
	}, nil
}

// typeSpec finds the declaration of the type name in file.
func typeSpec(file *ast.File, name string) (*ast.GenDecl, *ast.TypeSpec) {
	for _, d := range file.Decls {
		gen, ok := d.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, s := range gen.Specs {
			if ts := s.(*ast.TypeSpec); ts.Name.Name == name {
				return gen, ts
			}
		}
	}
	return nil, nil
}

// signature spells sig, a method's signature without func or its name,
// as st's file must: (bed string, litres int) error.
func signature(st *site.Site, sig *types.Signature) (string, error) {
	params, err := tuple(st, sig.Params(), sig.Variadic())
	if err != nil {
		return "", err
	}
	results, err := tuple(st, sig.Results(), false)
	if err != nil {
		return "", err
	}
	switch {
	case sig.Results().Len() == 0:
		return "(" + params + ")", nil
	case sig.Results().Len() == 1 && sig.Results().At(0).Name() == "":
		return "(" + params + ") " + results, nil
	}
	return "(" + params + ") (" + results + ")", nil
}

// tuple spells the variables of t, with their names when they have them;
// with variadic, the last as ...T.
func tuple(st *site.Site, t *types.Tuple, variadic bool) (string, error) {
	imports := map[string]*types.Package{}
	for path, p := range st.Pkg.Imports {
		imports[path] = p.Types
	}
	var parts []string
	for i := range t.Len() {
		v := t.At(i)
		typ := v.Type()
		dots := ""
		if variadic && i == t.Len()-1 {
			typ, dots = typ.(*types.Slice).Elem(), "..."
		}
		s, missing := analysis.TypeString(typ, st.File, st.Pkg.Types, imports)
		if missing != "" {
			return "", refactor.Refusef("the interface needs package %s, which %s does not import; import it first", missing, st.WS.Rel(st.TF.Name()))
		}
		if v.Name() != "" {
			s = v.Name() + " " + dots + s
		} else {
			s = dots + s
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", "), nil
}

// lineStart returns the offset of the start of the line off is on.
func lineStart(src []byte, off int) int {
	for off > 0 && src[off-1] != '\n' {
		off--
	}
	return off
}

// methodList names methods in a sentence: "the method Water", "the
// methods Water, Rate and Off".
func methodList(methods []string) string {
	if len(methods) == 1 {
		return "the method " + methods[0]
	}
	return "the methods " + strings.Join(methods[:len(methods)-1], ", ") + " and " + methods[len(methods)-1]
}
