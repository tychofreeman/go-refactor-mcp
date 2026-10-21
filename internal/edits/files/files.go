// Package files implements move_file and delete_file: edits that move or
// delete files and directories of the module, which nothing else can.
package files

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// MoveFile is the "move_file" edit.
type MoveFile struct{}

// NewMove returns the move_file edit.
func NewMove() *MoveFile { return &MoveFile{} }

func (*MoveFile) Name() string { return "move_file" }

func (*MoveFile) Description() string {
	return `Move or rename a file or a directory of the module, with everything in it, byte for byte. This is an edit: it does not change what refers to the file, so a Go file moved to another package must compile there (to move Go declarations, use move_declaration).
Use it for test fixtures (a golden case's directory renamed), docs and data. Give file (a file or directory) and to (the new path, which must not exist). Like every step, the result must type-check; one undo moves it back.`
}

func (*MoveFile) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file": {"type": "string", "description": "Path of the file or directory to move, relative to the workspace root."},
    "to":   {"type": "string", "description": "Its new path, relative to the workspace root; it must not exist."}
  },
  "required": ["file", "to"],
  "additionalProperties": false
}`)
}

// ChangesBehavior marks this as an edit.
func (*MoveFile) ChangesBehavior() {}

// Plan checks preconditions and computes the edits.
func (*MoveFile) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File string `json:"file"`
		To   string `json:"to"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	from, to := ws.Abs(a.File), ws.Abs(a.To)
	if !ws.Contains(to) {
		return nil, refactor.Refusef("%s is outside the workspace %s", a.To, ws.Root)
	}
	if _, err := os.Stat(to); err == nil {
		return nil, refactor.Refusef("%s already exists; give a path that does not", ws.Rel(to))
	}
	if from == to || strings.HasPrefix(to, from+string(filepath.Separator)) {
		return nil, refactor.Refusef("cannot move %s into itself", ws.Rel(from))
	}
	paths, err := filesUnder(ws, from, a.File)
	if err != nil {
		return nil, err
	}
	var fes []edit.FileEdit
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		dst := filepath.Join(to, strings.TrimPrefix(p, from))
		fes = append(fes,
			edit.FileEdit{Path: dst, Edits: []edit.TextEdit{{NewText: string(data)}}, Verbatim: true},
			edit.FileEdit{Path: p, Delete: true})
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("moved %s to %s%s", ws.Rel(from), ws.Rel(to), count(paths)),
		Edits:   fes,
	}, nil
}

// DeleteFile is the "delete_file" edit.
type DeleteFile struct{}

// NewDelete returns the delete_file edit.
func NewDelete() *DeleteFile { return &DeleteFile{} }

func (*DeleteFile) Name() string { return "delete_file" }

func (*DeleteFile) Description() string {
	return `Delete a file or a directory of the module, with everything in it. This is an edit; to delete Go declarations nothing uses, remove_dead_code checks that they are unused.
Use it for test fixtures (a refused.txt a case no longer needs, a case's directory), docs and data. Give file (a file or directory). Like every step, the result must type-check, so a Go file whose declarations are used is refused; one undo puts it all back.`
}

func (*DeleteFile) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file": {"type": "string", "description": "Path of the file or directory to delete, relative to the workspace root."}
  },
  "required": ["file"],
  "additionalProperties": false
}`)
}

// ChangesBehavior marks this as an edit.
func (*DeleteFile) ChangesBehavior() {}

// Plan checks preconditions and computes the edits.
func (*DeleteFile) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File string `json:"file"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	paths, err := filesUnder(ws, path, a.File)
	if err != nil {
		return nil, err
	}
	var fes []edit.FileEdit
	for _, p := range paths {
		fes = append(fes, edit.FileEdit{Path: p, Delete: true})
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("deleted %s%s", ws.Rel(path), count(paths)),
		Edits:   fes,
	}, nil
}

// filesUnder is path, a file, or the files under path, a directory: a
// path of the workspace that exists, given as given.
func filesUnder(ws *workspace.Workspace, path, given string) ([]string, error) {
	if !ws.Contains(path) || path == ws.Root {
		return nil, refactor.Refusef("%s is not a file or directory inside the workspace %s", given, ws.Root)
	}
	fi, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, refactor.Refusef("%s does not exist", given)
	}
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return []string{path}, nil
	}
	var out []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			out = append(out, p)
		}
		return err
	})
	if err == nil && len(out) == 0 {
		return nil, refactor.Refusef("%s has no files", given)
	}
	return out, err
}

// count says how many files a directory's change covered, for its summary.
func count(paths []string) string {
	if len(paths) == 1 {
		return ""
	}
	return fmt.Sprintf(" (%d file%s)", len(paths), textutil.Plural(len(paths)))
}
