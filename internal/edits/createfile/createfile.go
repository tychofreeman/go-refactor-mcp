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
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// CreateFile is the "create_file" edit.
type CreateFile struct{}

// New returns the create_file edit.
func New() *CreateFile { return &CreateFile{} }

func (*CreateFile) Name() string { return "create_file" }

func (*CreateFile) Description() string {
	return `Create a new file, Go or not (go.mod, JSON, a test fixture), with any missing directories: an edit. A Go file must parse and match its directory's package (a test may use the _test package), and is gofmt-ed. What it calls that nothing declares gets a stub, so a test written first compiles and fails: cart_test.go with if got := (&Cart{}).Total(); got != 0 { ... } also declares func (c *Cart) Total() int.
For several files, such as a fixture directory, send the create_file steps together in one batch. The file must not exist, unless overwrite is set to replace all of it; undo removes or restores it.`
}

func (*CreateFile) InputSchema() json.RawMessage {
	return refactor.Schema[createArgs]()
}

func (*CreateFile) ChangesBehavior() {}

type createArgs struct {
	File      string `json:"file" desc:"Path of the new file, relative to the workspace root."`
	Content   string `json:"content" desc:"The file's complete contents."`
	Overwrite bool   `json:"overwrite,omitempty" desc:"Replace the file if it exists."`
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
	old, err := os.ReadFile(path)
	exists := err == nil
	if exists && !a.Overwrite {
		return nil, refactor.Refusef("%s already exists; change it with the other tools, or set overwrite to replace it", rel)
	}
	// Missing directories are created (and removed again by undo), but a
	// file in the way is not replaced.
	for dir := filepath.Dir(path); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		fi, err := os.Stat(dir)
		if err != nil {
			continue // missing, or under a file: look further up
		}
		if !fi.IsDir() {
			return nil, refactor.Refusef("%s is a file, not a directory", ws.Rel(dir))
		}
		break
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
	did := "created"
	if exists {
		did = "replaced"
	}
	return &refactor.Plan{
		Summary:  fmt.Sprintf("%s %s (%d line%s)", did, rel, lines, textutil.Plural(lines)),
		Edits:    []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{End: len(old), NewText: a.Content}}}},
		Supplied: a.Content,
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
	if os.IsNotExist(err) {
		return "", nil // a directory the file creates
	}
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
