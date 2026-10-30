// Package fiximports is fix_imports for TypeScript, which the engine runs
// after an edit: it imports the names a file uses that another file
// exports, and removes the imports nothing uses. It is not a tool of its
// own.
package fiximports

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// FixImports fixes a file's imports.
type FixImports struct{ refactor.Named[fixImportsArgs] }

// New returns fix_imports.
func New() *FixImports { return &FixImports{Named: refactor.Called[fixImportsArgs]("fix_imports")} }

func (*FixImports) Description() string {
	return "Import the names a file uses that another file exports, and remove the imports nothing uses."
}

// fixImportsArgs are the arguments of fix_imports.
type fixImportsArgs struct {
	File string `json:"file" desc:"Relative to the root."`
}

func (*FixImports) ChangesBehavior() {}

func (*FixImports) Fixes() {}

// importError is an error an import fix may clear: a name not declared,
// which another file may export, or an import nothing uses.
var importError = regexp.MustCompile(`^(.+\.(?:ts|tsx|mts|cts)):\d+:\d+: (?:Cannot find name '([\w$]+)'|.* is declared but its value is never read|All imports in import declaration are unused)`)

// Clears returns the file of err when it is an error an import fix may
// clear.
func (*FixImports) Clears(err string) (string, bool) {
	if m := importError.FindStringSubmatch(err); m != nil {
		return m[1], true
	}
	return "", false
}

func (*FixImports) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a fixImportsArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	rel := ws.Rel(path)
	var names []string
	unused := false
	for _, e := range ws.Errors() {
		m := importError.FindStringSubmatch(e)
		switch {
		case m == nil || m[1] != rel:
		case m[2] == "":
			unused = true
		case !slices.Contains(names, m[2]):
			names = append(names, m[2])
		}
	}
	plan := &refactor.Plan{}
	var imports []tsapi.Import
	var imported []string
	for _, n := range names {
		switch ex := ws.Exporters(path, n); len(ex) {
		case 0:
		case 1:
			imports = append(imports, ex[0])
			imported = append(imported, fmt.Sprintf("%s from %s", n, ws.Rel(ex[0].From)))
		default:
			var from []string
			for _, x := range ex {
				from = append(from, ws.Rel(x.From))
			}
			plan.Notes = append(plan.Notes, fmt.Sprintf("%s was not imported: %s export it; add the import you mean", n, strings.Join(from, " and ")))
		}
	}
	fe := edit.FileEdit{Path: path}
	var did []string
	if unused {
		if removal := ws.RemoveUnusedImports(path); len(removal.Edits) > 0 {
			fe.Edits = removal.Edits
			did = append(did, "removed the imports nothing uses")
		}
	}
	if len(imports) > 0 {
		add, why := ws.AddImports(path, imports)
		if why != "" {
			return nil, refactor.Refusef("%s", why)
		}
		if overlaps(fe.Edits, add.Edits) {
			fe.Edits, did = nil, nil
		}
		fe.Edits = append(fe.Edits, add.Edits...)
		did = append([]string{"imported " + strings.Join(imported, ", ")}, did...)
	}
	if len(fe.Edits) == 0 {
		return nil, refactor.Refusef("no import to add to or remove from %s", rel)
	}
	plan.Summary = strings.Join(did, "; ") + " in " + rel
	plan.Edits = []edit.FileEdit{fe}
	return plan, nil
}

// overlaps reports whether an edit of a touches one of b.
func overlaps(a, b []edit.TextEdit) bool {
	for _, x := range a {
		for _, y := range b {
			if x.Start <= y.End && y.Start <= x.End {
				return true
			}
		}
	}
	return false
}
