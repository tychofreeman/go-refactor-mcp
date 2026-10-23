package switchmethod

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// PrepareForTypes is the "prepare_switch_for_types" refactoring.
type PrepareForTypes struct{}

// NewPrepareForTypes returns the prepare_switch_for_types refactoring.
func NewPrepareForTypes() *PrepareForTypes { return &PrepareForTypes{} }

func (*PrepareForTypes) Name() string { return "prepare_switch_for_types" }

func (*PrepareForTypes) Description() string {
	return `Prepare a switch on a value for its cases to move into types one at a time, each case becoming the method of a type that implements an interface: a value that implements the interface runs its method, and the switch handles the rest. No type implements it yet, so nothing changes until a case moves. With interface Species and method Water,
  func Water(p Plant) int { switch p.Species { case "fern": return 3; case "cactus": return 1 }; return 0 }
becomes
  type Species interface { Water() int }
  func Water(p Plant) int { if s, ok := p.Species.(Species); ok { return s.Water() }; switch p.Species { ... }; return 0 }
The switch's tag must be an interface (change_field_type to any first). The interface is declared before the function, or gets the method when it exists. The variables the cases use become the method's parameters. Every case must return, or none (then the switch goes in the else). A case of several values (case "fern", "cactus":) is split into a case for each, with a copy of the body, so that each moves alone.
Identify the switch by file and line, or by function when it is that function's only switch.
Refuses, saying why: a type that already implements the interface with the method, a case that leaves other than by return or assigns a variable it uses (it could not become a method), a case of several values that declares a label or a type (its body is copied), a tag that would be evaluated again with another value.`
}

func (*PrepareForTypes) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":      {"type": "string",  "description": "Path of the file with the switch, relative to the workspace root."},
    "line":      {"type": "integer", "description": "1-based line on which the switch statement starts."},
    "function":  {"type": "string",  "description": "Instead of line: the function (Name or Type.Method) whose only switch it is."},
    "interface": {"type": "string",  "description": "The interface: declared before the function if the package has none of that name."},
    "method":    {"type": "string",  "description": "Name of the interface's new method."}
  },
  "required": ["file", "interface", "method"],
  "additionalProperties": false
}`)
}

type prepareArgs struct {
	args
	Interface string `json:"interface"`
}

// A preparer readies a switch on a value for its cases to move into
// types: a value that implements the interface runs its method first.
type preparer struct {
	*switcher
	name string // the interface's
}

// Plan checks preconditions and computes the edits.
func (*PrepareForTypes) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a prepareArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	for _, n := range []string{a.Interface, a.Method} {
		if !token.IsIdentifier(n) {
			return nil, refactor.Refusef("%q is not a valid Go identifier", n)
		}
	}
	s, err := find(ws, a.args, false)
	if err != nil {
		return nil, err
	}
	g := &preparer{switcher: s, name: a.Interface}
	for _, check := range []func() error{g.checkTag, g.collectCases, g.checkInterface, g.checkBodies, g.checkSplit, g.checkUnimplemented} {
		if err := check(); err != nil {
			return nil, err
		}
	}
	return g.plan()
}

// checkTag checks the switch: it has a tag of an interface type, which
// the new code can evaluate again.
func (g *preparer) checkTag() error {
	sw := g.stmt.(*ast.SwitchStmt)
	if err := g.checkPlace(sw.Init); err != nil {
		return err
	}
	if sw.Tag == nil {
		return refactor.Refusef("the switch at %s has no tag, so there is no value whose cases could move into types", g.at(sw))
	}
	g.subject = sw.Tag
	if t := g.info.TypeOf(sw.Tag); !types.IsInterface(t) {
		return refactor.Refusef("%s is a %s, not an interface, so no value of it can implement %s; widen it first (change_field_type to any)", g.text(sw.Tag), g.typeName(t), g.name)
	}
	return g.checkStable()
}

// collectCases takes every case but default: their code is the method's
// to come.
func (g *preparer) collectCases() error {
	for _, st := range g.block.List {
		if cc := st.(*ast.CaseClause); cc.List != nil {
			g.cases = append(g.cases, kase{clause: cc})
		}
	}
	if len(g.cases) == 0 {
		return refactor.Refusef("the switch at %s has no case", g.at(g.stmt))
	}
	return nil
}

// checkSplit refuses to split a case of several values whose body
// declares a label or a type: each value gets its own case, with a copy
// of the body.
func (g *preparer) checkSplit() error {
	for _, c := range g.cases {
		if len(c.clause.List) < 2 {
			continue
		}
		var what string
		for _, st := range c.clause.Body {
			ast.Inspect(st, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.LabeledStmt:
					what = "label " + n.Label.Name + ", which a function cannot declare twice"
				case *ast.TypeSpec:
					what = "type " + n.Name.Name + ", which would be a different type in each copy"
				}
				return what == ""
			})
		}
		if what != "" {
			return refactor.Refusef("the case %s at %s declares %s; splitting it into a case for each value copies its body (extract_function the body first)",
				g.values(c.clause), g.at(c.clause), what)
		}
	}
	return nil
}

// values is the source of a case's values.
func (g *preparer) values(cc *ast.CaseClause) string {
	return string(g.src[g.off(cc.List[0].Pos()):g.off(cc.List[len(cc.List)-1].End())])
}

// split returns the switch's source with each case of several values
// split into a case for each, with a copy of the body, so that each
// value's code can move into its type alone; and the cases it split.
func (g *preparer) split() (string, []string) {
	at := g.off(g.stmt.Pos())
	ind := g.indent()
	var edits []edit.TextEdit
	var split []string
	for _, c := range g.cases {
		cc := c.clause
		if len(cc.List) < 2 {
			continue
		}
		body := g.body(cc, ind+"\t")
		var b strings.Builder
		for _, v := range cc.List[:len(cc.List)-1] {
			b.WriteString("case " + g.text(v) + ":\n" + body + ind)
		}
		b.WriteString("case " + g.text(cc.List[len(cc.List)-1]) + ":")
		edits = append(edits, edit.TextEdit{Start: g.off(cc.Case) - at, End: g.off(cc.Colon) + 1 - at, NewText: b.String()})
		split = append(split, g.values(cc))
	}
	src := g.src[at:g.off(g.stmt.End())]
	out, err := edit.Apply(src, edits)
	if err != nil {
		return string(src), nil
	}
	return string(out), split
}

// checkInterface finds the interface, or checks that its name is free.
func (g *preparer) checkInterface() error {
	o := g.pkg.Types.Scope().Lookup(g.name)
	if o == nil {
		if types.Universe.Lookup(g.name) != nil {
			return refactor.Refusef("%s is predeclared; choose another name for the interface", g.name)
		}
		for _, f := range g.pkg.Syntax {
			if s := g.info.Scopes[f]; s != nil && s.Lookup(g.name) != nil {
				return refactor.Refusef("%s is an import's name in %s; choose another name for the interface", g.name, g.ws.Rel(g.ws.Fset.Position(f.Pos()).Filename))
			}
		}
		return nil
	}
	tn, _ := o.(*types.TypeName)
	named, _ := o.Type().(*types.Named)
	if tn == nil || tn.IsAlias() || named == nil || !types.IsInterface(named) || named.TypeParams().Len() > 0 {
		return refactor.Refusef("%s is declared at %s, and is not an interface the types could implement; choose another name", g.name, g.ws.Position(o.Pos()))
	}
	it := named.Underlying().(*types.Interface)
	for i := range it.NumMethods() {
		if it.Method(i).Name() == g.method {
			return refactor.Refusef("interface %s already has a method %s", g.name, g.method)
		}
	}
	g.iface = named
	return nil
}

// checkUnimplemented refuses when a type already implements the
// interface with the method: a value of it in the tag would call the
// method instead of running the switch.
func (g *preparer) checkUnimplemented() error {
	var params []*types.Var
	for _, v := range g.params {
		params = append(params, types.NewParam(token.NoPos, g.pkg.Types, v.Name(), v.Type()))
	}
	results := g.info.Defs[g.fd.Name].(*types.Func).Signature().Results()
	sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), results, false)
	var embeds []types.Type
	if g.iface != nil {
		embeds = append(embeds, g.iface)
	}
	it := types.NewInterfaceType([]*types.Func{types.NewFunc(token.NoPos, g.pkg.Types, g.method, sig)}, embeds).Complete()
	named := g.namedTypes()
	if token.IsExported(g.method) {
		for _, d := range analysis.Dependencies(g.ws.Pkgs...) {
			for _, n := range d.Types.Scope().Names() {
				if tn, ok := d.Types.Scope().Lookup(n).(*types.TypeName); ok && !tn.IsAlias() {
					if nt, ok := tn.Type().(*types.Named); ok && !types.IsInterface(nt) {
						named = append(named, nt)
					}
				}
			}
		}
	}
	for _, n := range named {
		if types.Implements(n, it) || types.Implements(types.NewPointer(n), it) {
			return refactor.Refusef("type %s already implements %s with a method %s; a %s in %s would call it instead of running the switch",
				types.TypeString(n, types.RelativeTo(g.pkg.Types)), g.name, g.method, n.Obj().Name(), g.text(g.subject))
		}
	}
	return nil
}

// fresh returns base, or base with a number, a name that neither the
// switch nor the call uses.
func (g *preparer) fresh(base string) string {
	used := map[string]bool{}
	ast.Inspect(g.stmt, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			used[id.Name] = true
		}
		return true
	})
	for _, v := range g.params {
		used[v.Name()] = true
	}
	name := base
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s%d", base, i)
	}
	return name
}

// plan puts the call of the method before the switch, or the switch in
// its else, and
// declares the interface or adds the method to it.
func (g *preparer) plan() (*refactor.Plan, error) {
	path := g.ws.Fset.Position(g.fd.Pos()).Filename
	adder := imports.New(g.ws, g.pkg, g.file, g.src)
	q := func(p *types.Package) string {
		n, _ := adder.Name(p)
		return n
	}
	v, ok := g.fresh(strings.ToLower(g.name[:1])), g.fresh("ok")
	ind := g.indent()
	call := v + "." + g.method + "(" + g.args() + ")"
	head := fmt.Sprintf("if %s, %s := %s.(%s); %s {\n", v, ok, g.text(g.subject), g.name, ok)
	at, end := g.off(g.stmt.Pos()), g.off(g.stmt.End())
	sw, split := g.split()
	var e edit.TextEdit
	if returning, _ := g.returns(g.cases[0].clause); returning {
		e = edit.TextEdit{Start: at, End: end, NewText: head + ind + "\treturn " + call + "\n" + ind + "}\n" + ind + sw}
	} else {
		sw := textutil.Reindent(ind+sw, ind, ind+"\t")
		e = edit.TextEdit{Start: at, End: end, NewText: head + ind + "\t" + call + "\n" + ind + "} else {\n" + sw + "\n" + ind + "}"}
	}
	edits := []edit.TextEdit{e}
	var fes []edit.FileEdit
	summary := fmt.Sprintf("method %s added to %s", g.method, g.name)
	var notes []string
	if len(split) > 0 {
		notes = append(notes, "split into a case for each value, to move into its type alone: "+strings.Join(split, "; "))
	}
	if g.iface == nil {
		start := g.fd.Pos()
		if g.fd.Doc != nil {
			start = g.fd.Doc.Pos()
		}
		edits = append(edits, edit.TextEdit{Start: g.off(start), End: g.off(start), NewText: "type " + g.name + " interface {\n\t" + g.signature(q) + "\n}\n\n"})
		summary = fmt.Sprintf("%s declared, with method %s", g.name, g.method)
	} else {
		ifaceEdits, ifacePath, err := g.interfaceEdit(path, adder)
		if err != nil {
			return nil, err
		}
		if ifacePath == path {
			edits = append(edits, ifaceEdits...)
		} else {
			fes = append(fes, edit.FileEdit{Path: ifacePath, Edits: ifaceEdits})
		}
		if analysis.PublicAPI(g.iface.Obj(), g.ws.Pkgs) {
			notes = append(notes, refactor.APINote(g.name, "adding a method to it"))
		}
	}
	fes = append(fes, edit.FileEdit{Path: path, Edits: append(edits, adder.Edits()...)})
	return &refactor.Plan{
		Summary: fmt.Sprintf("prepared the switch of %s for its cases to move into types: a value of %s that implements %s runs its %s method, the switch the rest (%s); no type implements it yet",
			locate.FuncName(g.fd), g.text(g.subject), g.name, g.method, summary),
		Edits: edit.Merge(fes),
		Notes: notes,
	}, nil
}
