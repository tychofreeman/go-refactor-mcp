// Package fiximports implements fix_imports, which the engine runs after
// an edit, as it declares stubs: it adds the imports the file's code needs
// and removes those nothing uses any more. It is not a tool of its own.
package fiximports

import (
	"encoding/json"
	"go/ast"
	"go/types"
	"regexp"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// FixImports is the "fix_imports" edit.
type FixImports struct{}

// New returns the fix_imports edit.
func New() *FixImports { return &FixImports{} }

func (*FixImports) Name() string { return "fix_imports" }

func (*FixImports) Description() string {
	return "Add the imports a file's code needs, and remove those nothing uses."
}

func (*FixImports) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file": {"type": "string", "description": "Path of the file, relative to the workspace root."}
  },
  "required": ["file"],
  "additionalProperties": false
}`)
}

func (*FixImports) ChangesBehavior() {}

func (*FixImports) Fixes() {}

// importError is an error an import fix may clear: an import nothing
// uses, or a name that may be a package not imported.
var importError = regexp.MustCompile(`^(.+\.go):\d+:\d+: (".+" imported (as \w+ )?and not used|undefined: \w+)$`)

// Clears returns the file of err when it is an error an import fix may
// clear.
func (*FixImports) Clears(err string) (string, bool) {
	if m := importError.FindStringSubmatch(err); m != nil {
		return m[1], true
	}
	return "", false
}

// Plan checks preconditions and computes the edits.
func (*FixImports) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File string `json:"file"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	opened, err := site.At(ws, path)
	if err != nil {
		return nil, err
	}
	pkg, file, src := opened.Pkg, opened.File, opened.Src
	info := pkg.TypesInfo
	adder := imports.New(ws, pkg, file, src)

	// Imports nothing in the file uses.
	used := map[types.Object]bool{}
	for id, o := range info.Uses {
		if _, ok := o.(*types.PkgName); ok && file.FileStart <= id.Pos() && id.Pos() < file.FileEnd {
			used[o] = true
		}
	}
	var removed []string
	for _, spec := range file.Imports {
		var o types.Object
		switch {
		case spec.Name == nil:
			o = info.Implicits[spec]
		case spec.Name.Name != "_" && spec.Name.Name != ".":
			o = info.Defs[spec.Name]
		}
		if pn, ok := o.(*types.PkgName); ok && !used[pn] {
			adder.Remove(spec)
			removed = append(removed, pn.Imported().Path())
		}
	}

	// Package names the code uses that nothing declares: x in x.Name, where
	// x names one package unambiguously (as edits import them).
	var added []string
	tried := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || info.Uses[x] != nil || info.Defs[x] != nil || tried[x.Name] {
			return true
		}
		tried[x.Name] = true
		if paths, err := gocode.ImportNames(ws, pkg, file, adder, x.Pos(), []string{x.Name}, nil); err == nil {
			added = append(added, paths...)
		}
		return true
	})

	if len(added) == 0 && len(removed) == 0 {
		return nil, refactor.Refusef("%s has no import to add or remove", ws.Rel(path))
	}
	var notes []string
	if len(added) > 0 {
		notes = append(notes, gocode.ImportNote("added", added))
	}
	if len(removed) > 0 {
		notes = append(notes, gocode.ImportNote("removed", removed)+", which nothing uses any more")
	}
	return &refactor.Plan{
		Summary: "fixed the imports of " + ws.Rel(path),
		Edits:   []edit.FileEdit{{Path: path, Edits: adder.Edits()}},
		Notes:   notes,
	}, nil
}
