package refactor

import (
	"encoding/json"
	"path"
	"strings"
)

// MoveOf is the "move" tool of a workspace W.
type MoveOf[W Workspace] struct {
	Named[moveArgs]
	text string
}

// moveArgs are the arguments of move: those of the tools it stands for.
type moveArgs struct {
	File             string   `json:"file" desc:"Path of a file containing the declaration or method, or a use of it; or, without name, of the file or directory to move. Relative to the root."`
	Line             int      `json:"line,omitempty" desc:"Optional 1-based line of an occurrence of name. Omit it to find name as a top-level name of file."`
	Name             string   `json:"name,omitempty" desc:"What to move: without line, a top-level name or Type.Method; with line, the name at it. Omit it to move the file itself."`
	Column           int      `json:"column,omitempty" desc:"Optional 1-based column, only needed to disambiguate."`
	ToFile           string   `json:"to_file,omitempty" desc:"Where a declaration goes: a file, created if it does not exist; in another directory, another package."`
	To               string   `json:"to,omitempty" desc:"Where a method goes (the type), or, without name, the file's new path."`
	NewName          string   `json:"new_name,omitempty" desc:"Optional, for a declaration moving to another package: its name there."`
	Also             []string `json:"also,omitempty" desc:"Optional: more declarations (or methods) to move with it, by name."`
	WithDependencies bool     `json:"with_dependencies,omitempty" desc:"Optional, for a declaration: also move what it uses that would be left behind."`
	Copy             bool     `json:"copy,omitempty" desc:"Optional, for a file: copy it instead."`
	Delete           bool     `json:"delete,omitempty" desc:"Optional, for a file: delete it (or the directory, with everything in it) instead."`
}

// NewMove returns move, described by description.
func NewMove[W Workspace](description string) *MoveOf[W] {
	return &MoveOf[W]{Named: Called[moveArgs]("move"), text: description}
}

// Route names the tool args ask for: delete_file for delete,
// move_declaration for to_file, move_method for to with name, move_file
// for to without name.
func (m *MoveOf[W]) Route(args json.RawMessage) (string, error) {
	var a moveArgs
	if err := DecodeArgs(args, &a); err != nil {
		return "", err
	}
	if a.Delete {
		if a.To != "" || a.ToFile != "" || a.Name != "" || a.Copy {
			return "", Refusef("delete takes only file, the file or directory to delete; give it without to, to_file, name or copy")
		}
		return "delete_file", nil
	}
	switch {
	case a.ToFile != "" && a.To != "":
		return "", Refusef("give one destination: to_file to move a declaration to that file, or to to move a method to that type (with name) or the file at file to that path (without name)")
	case a.ToFile != "":
		if a.Name == "" {
			return "", Refusef("to_file moves a declaration: give its name too; to move the file itself, give to (its new path) without name")
		}
		return "move_declaration", nil
	case a.To != "" && a.Name != "":
		return "move_method", nil
	case a.To != "":
		if a.Line != 0 || a.Column != 0 || len(a.Also) > 0 || a.NewName != "" || a.WithDependencies {
			return "", Refusef("without name, move moves the file or directory at file to the path to, and takes only copy besides; to move a declaration, give name and to_file")
		}
		return "move_file", nil
	}
	return "", Refusef("give where it goes: to_file for a declaration (with name), to for a method (name Type.Method, to the type) or, without name, for the file or directory at file (to its new path)")
}

// Covers names the tools move stands for.
func (m *MoveOf[W]) Covers() []string {
	return []string{"move_declaration", "move_method", "move_file", "delete_file"}
}

// A Router is a Covering tool that chooses among the tools it stands for
// by its arguments alone: Route names the one they ask for, or refuses
// them. A Covering tool that is not a Router chooses in its Plan.
type Router interface {
	Covering
	Route(args json.RawMessage) (string, error)
}

// A Covering tool is published in place of the tools Covers names, and
// stands for them.
type Covering interface {
	Covers() []string
}

// Routed returns the tool r stands for with args when r is a Router, found
// by lookup, with args less what only r takes; otherwise, or when Route
// refuses args, r and args, whose Plan gives the refusal.
func Routed[W Workspace](r RefactoringOf[W], args json.RawMessage, lookup func(string) (RefactoringOf[W], bool)) (RefactoringOf[W], json.RawMessage) {
	rt, ok := r.(Router)
	if !ok {
		return r, args
	}
	name, err := rt.Route(args)
	if err != nil {
		return r, args
	}
	to, ok := lookup(name)
	if !ok {
		return r, args
	}
	return to, withoutOwn(args, r.InputSchema(), to.InputSchema())
}

// Published returns rs without the tools a Covering tool among them
// stands for.
func Published[W Workspace](rs []RefactoringOf[W]) []RefactoringOf[W] {
	covered := map[string]bool{}
	for _, r := range rs {
		if rt, ok := r.(Covering); ok {
			for _, name := range rt.Covers() {
				covered[name] = true
			}
		}
	}
	var out []RefactoringOf[W]
	for _, r := range rs {
		if !covered[r.Name()] {
			out = append(out, r)
		}
	}
	return out
}

// ChangeTypeRoute names the tool change_type's arguments ask for:
// change_parameter_type for parameter, change_field_type for values or a
// name Type.Field, and change_type itself otherwise.
func ChangeTypeRoute(args json.RawMessage) (string, error) {
	var a struct {
		Name, Parameter string
		Values          map[string]string
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", Refusef("invalid arguments: %v", err)
	}
	switch {
	case a.Parameter != "":
		return "change_parameter_type", nil
	case len(a.Values) > 0 || strings.Contains(a.Name, "."):
		return "change_field_type", nil
	}
	return "change_type", nil
}

// ChangeTypeCovers names the tools change_type stands for.
var ChangeTypeCovers = []string{"change_field_type", "change_parameter_type"}

// Description is move's, for the language it moves.
func (m *MoveOf[W]) Description() string { return m.text }

// Plan refuses: move runs the tool Route names, never itself, so it is
// reached only when Route refuses args or names a tool the language lacks.
func (m *MoveOf[W]) Plan(_ W, args json.RawMessage) (*Plan, error) {
	name, err := m.Route(args)
	if err != nil {
		return nil, err
	}
	return nil, Refusef("move would run %s, which this language does not have", name)
}

// FileRename returns move_file's arguments for renaming file to newName
// in its directory: what rename does with a file and no name. newName is
// a name, not a path; a path is refused, naming move.
func FileRename(file, newName string) (json.RawMessage, error) {
	if newName == "" || strings.ContainsAny(newName, `/\`) {
		return nil, Refusef("to rename the file %s, give new_name as a file name in its directory (%s); to put it elsewhere, use move with to", file, path.Base(file))
	}
	return json.Marshal(map[string]string{"file": file, "to": path.Join(path.Dir(file), newName)})
}
