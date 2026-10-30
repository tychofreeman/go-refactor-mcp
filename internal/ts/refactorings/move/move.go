// Package move has the TypeScript tools that move code between files:
// move_declaration, move_file and delete_file.
package move

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// MoveDeclaration moves top-level declarations to another file.
type MoveDeclaration struct {
	refactor.Named[moveDeclarationArgs]
}

// NewMoveDeclaration returns move_declaration.
func NewMoveDeclaration() *MoveDeclaration {
	return &MoveDeclaration{Named: refactor.Called[moveDeclarationArgs]("move_declaration")}
}

func (*MoveDeclaration) Description() string {
	return `Move a top-level declaration (function with its overloads, class, interface, type, enum, or a variable statement, with the comments above it) to another file, creating the file if needed: moving water from src/plant.ts to src/water.ts declares it there, exported if anything outside it uses it, and changes import { water } from "./plant" to import from "./water" in every file. Imports follow the code: what it uses of its old file's imports is imported at its new place, and an import only it used goes.
Identify the declaration by file and name, or by line at it or any use; to_file is the destination, relative to the root. also names more top-level declarations of the same file to move with it, such as the helpers it uses; with_dependencies moves every declaration of the file it uses, and what those use.
Refuses, saying why: code that uses a declaration that stays and is not exported (move it too, or export it first); a name the destination declares already; a default export or a namespace; a statement declaring several variables; a file that imports the old one as a namespace and uses what moves, or re-exports all of it; import() or require() of the old file.`
}

// moveDeclarationArgs are the arguments of move_declaration.
type moveDeclarationArgs struct {
	File     string   `json:"file" desc:"Path of a file containing the declaration or a use of it, relative to the root."`
	Line     int      `json:"line,omitempty" desc:"1-based line of that occurrence. Omit it to find name as a top-level declaration of file."`
	Name     string   `json:"name" desc:"The declared name."`
	Column   int      `json:"column,omitempty" desc:"Optional 1-based column, only needed to disambiguate."`
	To       string   `json:"to_file" desc:"Destination file, relative to the root; created if it does not exist."`
	Also     []string `json:"also,omitempty" desc:"Optional: other top-level declarations of the same file to move with it, by name, e.g. the helpers it uses."`
	WithDeps bool     `json:"with_dependencies,omitempty" desc:"Optional: also move every top-level declaration of the file that the moved code uses, and what those use, in place of naming them in also."`
	NewName  string   `json:"new_name" schema:"-"`
}

func (*MoveDeclaration) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a moveDeclarationArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.NewName != "" {
		return nil, refactor.Refusef("new_name is for Go's packages; in TypeScript, rename before or after the move")
	}
	path, to := ws.Abs(a.File), ws.Abs(a.To)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", a.File)
	}
	if !ws.Contains(to) {
		return nil, refactor.Refusef("%s is outside the root %s", a.To, ws.Root)
	}
	if ext := strings.ToLower(to[strings.LastIndex(to, "."):]); !strings.Contains(".ts .tsx .mts .cts", ext) || strings.HasSuffix(to, ".d.ts") {
		return nil, refactor.Refusef("%s is not a TypeScript source file", a.To)
	}
	_, statErr := os.Stat(to)
	created := statErr != nil
	edits, why := ws.Edits(path, func(p *tsapi.Project) ([]tsapi.Edit, string) {
		es, _, why := p.MoveDeclaration(context.Background(), path, a.Line, a.Name, a.Column, to, a.Also, a.WithDeps)
		return es, why
	})
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	emptied := false
	for i := range edits {
		if created && edits[i].Path == to {
			edits[i].Verbatim = true
		}
		// An old file left with nothing in it goes.
		if edits[i].Path == path {
			if before, err := os.ReadFile(path); err == nil {
				if after, err := edit.Apply(before, edits[i].Edits); err == nil && strings.TrimSpace(string(after)) == "" {
					edits[i] = edit.FileEdit{Path: path, Delete: true}
					emptied = true
				}
			}
		}
	}
	names := a.Name
	if len(a.Also) > 0 {
		names += ", " + strings.Join(a.Also, ", ")
	}
	summary := fmt.Sprintf("moved %s to %s", names, ws.Rel(to))
	if created {
		summary += " (created)"
	}
	if emptied {
		summary += fmt.Sprintf("; %s, left empty, is deleted", ws.Rel(path))
	}
	return &refactor.Plan{Summary: summary, Edits: edits}, nil
}

// MoveFile moves a file or directory, updating what refers to it.
type MoveFile struct{ refactor.Named[moveFileArgs] }

// NewMoveFile returns move_file.
func NewMoveFile() *MoveFile { return &MoveFile{Named: refactor.Called[moveFileArgs]("move_file")} }

func (*MoveFile) Description() string {
	return `Move or rename a file or directory, with everything in it, and update what refers to it: moving src/plant.ts to src/garden/plant.ts changes import { water } from "./plant" to "./garden/plant" in every file, and the moved file's own relative imports, as the language service's file rename does (the tsconfig's file lists too). Fixtures, docs and data move byte for byte. With copy the original stays and nothing that refers to it changes; the copy's relative imports are updated (a fixture directory copied starts a new case). Give file and to, which must not exist; one undo moves it back or removes the copy.`
}

// moveFileArgs are the arguments of move_file.
type moveFileArgs struct {
	File string `json:"file" desc:"Path of the file or directory to move, relative to the root."`
	To   string `json:"to" desc:"Its new path, relative to the root; it must not exist."`
	Copy bool   `json:"copy,omitempty" desc:"Keep the original: copy it to to instead."`
}

func (*MoveFile) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a moveFileArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	from, to := ws.Abs(a.File), ws.Abs(a.To)
	switch {
	case !ws.Contains(from) || from == ws.Root:
		return nil, refactor.Refusef("%s is not a file or directory inside the root %s", a.File, ws.Root)
	case !ws.Contains(to):
		return nil, refactor.Refusef("%s is outside the root %s", a.To, ws.Root)
	case from == to || strings.HasPrefix(to, from+string(os.PathSeparator)):
		return nil, refactor.Refusef("cannot move %s into itself", ws.Rel(from))
	}
	if _, err := os.Stat(from); err != nil {
		return nil, refactor.Refusef("%s does not exist", a.File)
	}
	if _, err := os.Stat(to); err == nil {
		return nil, refactor.Refusef("%s already exists; give a path that does not", ws.Rel(to))
	}
	edits, err := ws.MoveFile(from, to)
	if err != nil {
		return nil, err
	}
	if a.Copy {
		return copied(ws, from, to, edits), nil
	}
	moved, changed := 0, 0
	for _, fe := range edits {
		switch {
		case fe.Delete:
			moved++
		case !fe.Verbatim:
			changed++
		}
	}
	summary := fmt.Sprintf("moved %s to %s", ws.Rel(from), ws.Rel(to))
	if moved > 1 {
		summary += fmt.Sprintf(" (%d files)", moved)
	}
	if changed > 0 {
		summary += fmt.Sprintf(", updating %d file%s that refer to it", changed, map[bool]string{true: "", false: "s"}[changed == 1])
	}
	return &refactor.Plan{Summary: summary, Edits: edits}, nil
}

// copied is the plan that copies from to to: of the edits that move it,
// those that write the files under to, whose own relative imports they
// update; the original and what refers to it stay as they are.
func copied(ws *workspace.Workspace, from, to string, moving []edit.FileEdit) *refactor.Plan {
	var edits []edit.FileEdit
	files := map[string]bool{}
	for _, fe := range moving {
		if fe.Path == to || strings.HasPrefix(fe.Path, to+string(os.PathSeparator)) {
			edits = append(edits, fe)
			files[fe.Path] = true
		}
	}
	summary := fmt.Sprintf("copied %s to %s", ws.Rel(from), ws.Rel(to))
	if len(files) > 1 {
		summary += fmt.Sprintf(" (%d files)", len(files))
	}
	return &refactor.Plan{Summary: summary, Edits: edits}
}

// DeleteFile deletes a file or directory nothing imports.
type DeleteFile struct{ refactor.Named[deleteFileArgs] }

// NewDeleteFile returns delete_file.
func NewDeleteFile() *DeleteFile {
	return &DeleteFile{Named: refactor.Called[deleteFileArgs]("delete_file")}
}

func (*DeleteFile) Description() string {
	return `Delete a file or directory, with everything in it: for fixtures, docs and data, and for source files nothing imports any more. A file another file still imports (or a directory whose files are imported from outside it) is refused, naming the imports; to delete declarations, remove_dead_code checks that nothing uses them. Give file; one undo puts it all back.`
}

// deleteFileArgs are the arguments of delete_file.
type deleteFileArgs struct {
	File string `json:"file" desc:"Path of the file or directory to delete, relative to the root."`
}

func (*DeleteFile) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a deleteFileArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	if !ws.Contains(path) || path == ws.Root {
		return nil, refactor.Refusef("%s is not a file or directory inside the root %s", a.File, ws.Root)
	}
	paths, err := workspace.Under(path)
	if os.IsNotExist(err) {
		return nil, refactor.Refusef("%s does not exist", a.File)
	}
	if err != nil {
		return nil, err
	}
	if at := ws.Importers(path); len(at) > 0 {
		more := ""
		if len(at) > 3 {
			more = fmt.Sprintf(" and %d more", len(at)-3)
			at = at[:3]
		}
		return nil, refactor.Refusef("%s is imported at %s%s; change those first", a.File, strings.Join(at, ", "), more)
	}
	plan := &refactor.Plan{Summary: fmt.Sprintf("deleted %s", ws.Rel(path))}
	for _, p := range paths {
		plan.Edits = append(plan.Edits, edit.FileEdit{Path: p, Delete: true})
	}
	if len(paths) > 1 {
		plan.Summary += fmt.Sprintf(" (%d files)", len(paths))
	}
	return plan, nil
}
