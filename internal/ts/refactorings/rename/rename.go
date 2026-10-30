// Package rename is rename for TypeScript: through the compiler's language
// service, in every project of the solution, with the checks a model
// needs the tool to make.
package rename

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// Rename renames a symbol and every reference to it.
type Rename struct{}

// New returns rename for TypeScript.
func New() *Rename { return &Rename{} }

func (*Rename) Name() string { return "rename" }

func (*Rename) Description() string {
	return `Rename a function, class, interface, type, variable, parameter, property or method, and every reference to it in every project of the solution, tests included: renaming water in src/plant.ts to irrigate changes its declaration, each import { water } and each call. Identify it by file, line and its current name (column if the name is on the line twice); without line, name is a top-level declaration of file or a member (Bed.plant), and with function a local of that function ("Bed.plant", name "rows").
Refuses, saying why: a new name that is not an identifier or is reserved, or that a file the rename changes already uses; a member of a type whose values reach code that finds properties by name (JSON.stringify, Object.keys, for...in, "name" in x; what console prints is listed in a note); what the language service cannot rename (a library's declaration, a keyword).`
}

func (*Rename) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":     {"type": "string",  "description": "Path of a file with the symbol, relative to the root."},
    "line":     {"type": "integer", "description": "1-based line of an occurrence. Omit it to find name as a top-level declaration of file, or Class.member."},
    "function": {"type": "string",  "description": "Instead of line, for a local: the function (name or Class.method) of file that declares it (a parameter, variable or constant)."},
    "name":     {"type": "string",  "description": "The current name."},
    "column":   {"type": "integer", "description": "Optional 1-based column, only needed when the name is on the line twice."},
    "new_name": {"type": "string",  "description": "The new name."}
  },
  "required": ["file", "name", "new_name"],
  "additionalProperties": false
}`)
}

type args struct {
	File     string `json:"file"`
	Function string `json:"function"`
	Line     int    `json:"line"`
	Name     string `json:"name"`
	Column   int    `json:"column"`
	NewName  string `json:"new_name"`
}

var identifier = regexp.MustCompile(`^[\p{L}_$][\p{L}\p{N}_$]*$`)

// reserved are the words that cannot name a declaration.
var reserved = strings.Fields(`break case catch class const continue debugger default delete do else enum export extends false finally for function if import in instanceof new null return super switch this throw true try typeof var void while with
	implements interface let package private protected public static yield await`)

func (*Rename) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	switch {
	case !identifier.MatchString(a.NewName):
		return nil, refactor.Refusef("%q is not a valid name", a.NewName)
	case slices.Contains(reserved, a.NewName):
		return nil, refactor.Refusef("%q is a reserved word", a.NewName)
	case a.NewName == a.Name:
		return nil, refactor.Refusef("the new name is the name it has")
	}
	path := ws.Abs(a.File)
	offset, err := locate(ws, path, a)
	if err != nil {
		return nil, err
	}
	keyed, printed := ws.KeyedUses(path, offset)
	if keyed != "" {
		return nil, refactor.Refusef("%s", keyed)
	}
	edits, why, err := ws.Rename(path, offset, a.NewName)
	if err != nil {
		return nil, err
	}
	if why != "" {
		return nil, refactor.Refusef("cannot rename %q: %s", a.Name, why)
	}
	if len(edits) == 0 {
		return nil, refactor.Refusef("the language service found nothing to rename at %q in %s", a.Name, a.File)
	}
	n := 0
	for _, fe := range edits {
		n += len(fe.Edits)
		if where := usedIn(ws, fe, a.NewName); where != "" {
			return nil, refactor.Refusef("%q is already used in %s, so the rename could change what a name refers to; choose another name", a.NewName, where)
		}
	}
	plan := &refactor.Plan{
		Summary:   fmt.Sprintf("renamed %q to %q (%d occurrence%s in %d file%s)", a.Name, a.NewName, n, textutil.Plural(n), len(edits), textutil.Plural(len(edits))),
		Edits:     edits,
		Locations: true,
	}
	if len(printed) > 0 {
		plan.Notes = append(plan.Notes, "what these calls print changes, since they print property names: "+textutil.FirstFew(printed, 5))
	}
	return plan, nil
}

// locate returns the offset of the occurrence of a.Name that a names:
// on a.Line (at a.Column if given), or the top-level declaration.
func locate(ws *workspace.Workspace, path string, a args) (int, error) {
	if _, err := os.Stat(path); err != nil {
		return 0, refactor.Refusef("%s: no such file", a.File)
	}
	if a.Function != "" && a.Line == 0 {
		off, why := ws.Local(path, a.Function, a.Name)
		if why != "" {
			return 0, refactor.Refusef("%s", why)
		}
		return off, nil
	}
	if a.Line == 0 {
		if off, ok := ws.TopLevel(path, a.Name); ok {
			return off, nil
		}
		return 0, refactor.Refusef("%s declares no top-level %q; give the line of an occurrence", a.File, a.Name)
	}
	var on []int
	var cols []string
	for _, id := range ws.Identifiers(path) {
		if id.Name == a.Name && id.Line == a.Line && (a.Column == 0 || id.Column == a.Column) {
			on = append(on, id.Offset)
			cols = append(cols, fmt.Sprint(id.Column))
		}
	}
	switch len(on) {
	case 0:
		return 0, refactor.Refusef("no %q on line %d of %s", a.Name, a.Line, a.File)
	case 1:
		return on[0], nil
	}
	return 0, refactor.Refusef("%q is on line %d of %s %d times, at columns %s: give column", a.Name, a.Line, a.File, len(on), strings.Join(cols, ", "))
}

// usedIn returns where the file fe edits already has an identifier
// called name, as "file:line", or "".
func usedIn(ws *workspace.Workspace, fe edit.FileEdit, name string) string {
	for _, id := range ws.Identifiers(fe.Path) {
		if id.Name == name {
			return fmt.Sprintf("%s:%d", ws.Rel(fe.Path), id.Line)
		}
	}
	return ""
}
