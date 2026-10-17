// Package createfile implements create_file, an edit: it may change
// behavior.
package createfile

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// CreateFile is the "create_file" edit.
type CreateFile struct{}

// New returns the create_file edit.
func New() *CreateFile { return &CreateFile{} }

func (*CreateFile) Name() string { return "create_file" }

func (*CreateFile) Description() string {
	return `Create a new file with the given content. This is an edit, not a refactoring: it may change behavior.
Use it to add code that does not exist yet, such as a new type, a test, or a new package's first file; use the refactorings to change existing code.
A Go file must parse, and its package clause must match the other Go files in its directory (a test file may use the _test package); it is gofmt-ed. The directory must exist, and the file must not.
Like every step, the result must type-check: if the new file refers to something that does not exist yet, it is refused with the errors. To write a failing test first, first declare what it calls (a stub), so the test compiles and fails. Undo removes the file.`
}

func (*CreateFile) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":    {"type": "string", "description": "Path of the new file, relative to the workspace root."},
    "content": {"type": "string", "description": "The file's complete contents."}
  },
  "required": ["file", "content"],
  "additionalProperties": false
}`)
}

func (*CreateFile) ChangesBehavior() {}

type createArgs struct {
	File    string `json:"file"`
	Content string `json:"content"`
}

// Plan checks preconditions and computes the edits.
func (*CreateFile) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a createArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.File == "" {
		return nil, refactor.Refusef("file is required")
	}
	path := ws.Abs(a.File)
	rel := ws.Rel(path)
	if !ws.Contains(path) {
		return nil, refactor.Refusef("%s is outside the workspace %s", a.File, ws.Root)
	}
	if _, err := os.Stat(path); err == nil {
		return nil, refactor.Refusef("%s already exists; change it with the other tools", rel)
	}
	dir := filepath.Dir(path)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return nil, refactor.Refusef("the directory %s does not exist; creating directories is not supported yet", ws.Rel(dir))
	}
	if strings.HasSuffix(path, ".go") {
		if err := checkGo(ws, path, a.Content); err != nil {
			return nil, err
		}
	}
	lines := strings.Count(a.Content, "\n")
	if a.Content != "" && !strings.HasSuffix(a.Content, "\n") {
		lines++
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("created %s (%d line%s)", rel, lines, plural(lines)),
		Edits:   []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{NewText: a.Content}}}},
	}, nil
}

// checkGo refuses Go source that does not parse, or whose package clause
// does not match the other Go files in its directory.
func checkGo(ws *workspace.Workspace, path, content string) error {
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Base(path), content, parser.AllErrors)
	if err != nil {
		return refactor.Refusef("%s does not parse: %v", ws.Rel(path), err)
	}
	name := f.Name.Name
	want, err := dirPackage(filepath.Dir(path))
	if err != nil {
		return err
	}
	ok := want == "" || name == want || (strings.HasSuffix(path, "_test.go") && name == want+"_test")
	if !ok {
		return refactor.Refusef("%s is package %s, but the other files in %s are package %s", ws.Rel(path), name, ws.Rel(filepath.Dir(path)), want)
	}
	return nil
}

// dirPackage returns the package name of the Go files in dir ("" if
// there are none), ignoring external test packages.
func dirPackage(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	test := ""
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, parser.PackageClauseOnly)
		if err != nil {
			continue
		}
		switch {
		case !strings.HasSuffix(e.Name(), "_test.go"):
			return f.Name.Name, nil
		case !strings.HasSuffix(f.Name.Name, "_test"):
			test = f.Name.Name
		}
	}
	return test, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
