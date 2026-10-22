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

// Guard is the "add_interface_guard" refactoring.
type Guard struct{}

// NewGuard returns the add_interface_guard refactoring.
func NewGuard() *Guard { return &Guard{} }

func (*Guard) Name() string { return "add_interface_guard" }

func (*Guard) Description() string {
	return `Put before a switch on a value a call of an interface's method, for a value that implements the interface: the first step of turning the cases into types one at a time. Nothing implements the method yet, so the switch still does everything. With interface Kind and method Days,
  func Days(it Item) int { switch it.Kind { case "book": return 21; case "dvd": return 7 }; return 0 }
becomes
  type Kind interface { Days() int }
  func Days(it Item) int { if k, ok := it.Kind.(Kind); ok { return k.Days() }; switch it.Kind { ... }; return 0 }
The switch's tag must be an interface (change_field_type to any first). The interface is declared before the function, or gets the method when it exists. The variables the cases use become the method's parameters. Every case must return, or none (then the switch goes in the else).
Identify the switch by file and line, or by function when it is that function's only switch.
Refuses, saying why: a type that already implements the interface with the method, a case that leaves other than by return or assigns a variable it uses, a tag the guard would evaluate again with another value.`
}

func (*Guard) InputSchema() json.RawMessage {
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

type guardArgs struct {
	args
	Interface string `json:"interface"`
}

// A guard is a switch on a value that gets an interface's method called
// before it.
type guard struct {
	*switcher
	name string // the interface's
}

// Plan checks preconditions and computes the edits.
func (*Guard) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a guardArgs
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
	g := &guard{switcher: s, name: a.Interface}
	for _, check := range []func() error{g.checkTag, g.collectCases, g.checkInterface, g.checkBodies, g.checkUnimplemented} {
		if err := check(); err != nil {
			return nil, err
		}
	}
	return g.plan()
}

// checkTag checks the switch: it has a tag of an interface type, which
// the guard can evaluate again.
func (g *guard) checkTag() error {
	sw := g.stmt.(*ast.SwitchStmt)
	if err := g.checkPlace(sw.Init); err != nil {
		return err
	}
	if sw.Tag == nil {
		return refactor.Refusef("the switch at %s has no tag; the guard needs a value to test", g.at(sw))
	}
	g.subject = sw.Tag
	if t := g.info.TypeOf(sw.Tag); !types.IsInterface(t) {
		return refactor.Refusef("%s is a %s, not an interface, so no value of it can implement %s; widen it first (change_field_type to any)", g.text(sw.Tag), g.typeName(t), g.name)
	}
	return g.checkStable()
}

// collectCases takes every case but default: their code is the method's
// to come.
func (g *guard) collectCases() error {
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

// checkInterface finds the interface, or checks that its name is free.
func (g *guard) checkInterface() error {
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
		return refactor.Refusef("%s is declared at %s, and is not an interface the guard can use; choose another name", g.name, g.ws.Position(o.Pos()))
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
func (g *guard) checkUnimplemented() error {
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
func (g *guard) fresh(base string) string {
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

// plan puts the guard before the switch, or the switch in its else, and
// declares the interface or adds the method to it.
func (g *guard) plan() (*refactor.Plan, error) {
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
	var e edit.TextEdit
	if returning, _ := g.returns(g.cases[0].clause); returning {
		e = edit.TextEdit{Start: at, End: at, NewText: head + ind + "\treturn " + call + "\n" + ind + "}\n" + ind}
	} else {
		sw := textutil.Reindent(ind+g.text(g.stmt), ind, ind+"\t")
		e = edit.TextEdit{Start: at, End: end, NewText: head + ind + "\t" + call + "\n" + ind + "} else {\n" + sw + "\n" + ind + "}"}
	}
	edits := []edit.TextEdit{e}
	var fes []edit.FileEdit
	summary := fmt.Sprintf("added method %s to interface %s", g.method, g.name)
	var notes []string
	if g.iface == nil {
		start := g.fd.Pos()
		if g.fd.Doc != nil {
			start = g.fd.Doc.Pos()
		}
		edits = append(edits, edit.TextEdit{Start: g.off(start), End: g.off(start), NewText: "type " + g.name + " interface {\n\t" + g.signature(q) + "\n}\n\n"})
		summary = fmt.Sprintf("declared interface %s with method %s", g.name, g.method)
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
		Summary: fmt.Sprintf("%s, and before the switch of %s a guard that calls it for a value of %s that implements %s (nothing does yet)", summary, locate.FuncName(g.fd), g.text(g.subject), g.name),
		Edits:   edit.Merge(fes),
		Notes:   notes,
	}, nil
}
