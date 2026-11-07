// Package createfile is create_file for a TypeScript solution: an edit
// that adds a file, source or not.
package createfile

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// CreateFile creates a file.
type CreateFile struct{ refactor.Named[createFileArgs] }

// New returns create_file.
func New() *CreateFile { return &CreateFile{Named: refactor.Called[createFileArgs]("create_file")} }

func (*CreateFile) ChangesBehavior() {}

func (*CreateFile) Description() string {
	return `Create a new file, TypeScript or not (a fixture, JSON), with any missing directories. A source file a project's include takes joins that project; what it adds must type-check, so a test written first goes with what it calls, in one batch with check "end".
With declaration in place of content, a class, interface or enum from its header: "export class Bed extends Soil implements Planter" writes the class with, for each method its interfaces and abstract base require, the method throwing new Error("not implemented"), and each required property with a definite-assignment mark (readonly size!: number), importing what they name. With test_for, a test file with a red test of a function or Class.method, as add_declaration's test_for writes it.
The file must not exist, unless overwrite is set to replace all of it; undo removes or restores it.`
}

// createFileArgs are the arguments of create_file.
type createFileArgs struct {
	File        string `json:"file" desc:"Path of the new file, relative to the root."`
	Content     string `json:"content,omitempty" desc:"The file's complete contents."`
	Declaration string `json:"declaration,omitempty" desc:"Instead of content: a class's, interface's or enum's header (export class Bed implements Planter); the file gets it with a body, a class's holding the members its interfaces and abstract base require."`
	TestFor     string `json:"test_for,omitempty" desc:"Instead of content, for a test file: the function or Class.method to write a red test for, with test imported from the solution's framework."`
	Overwrite   bool   `json:"overwrite,omitempty" desc:"Replace the file if it exists."`
}

func (*CreateFile) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a createFileArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	if !ws.Contains(path) {
		return nil, refactor.Refusef("%s is outside the root %s, or in node_modules", a.File, ws.Root)
	}
	old, err := os.ReadFile(path)
	exists := err == nil
	if exists && !a.Overwrite {
		return nil, refactor.Refusef("%s already exists; change it with the other tools, or set overwrite to replace it", a.File)
	}
	switch {
	case a.TestFor != "" && (a.Content != "" || a.Declaration != ""):
		return nil, refactor.Refusef("give one of content, declaration and test_for: test_for writes the test file from what it tests")
	case a.TestFor != "":
		test, module, why := ws.TestFor(a.TestFor)
		if why != "" {
			return nil, refactor.Refusef("%s", why)
		}
		a.Content = test
		if module != "" {
			a.Content = `import { test } from "` + module + `";` + "\n\n" + test
		}
	case a.Declaration != "" && a.Content != "":
		return nil, refactor.Refusef("give content or declaration, not both: declaration writes the file from a class's header")
	case a.Declaration != "":
		content, err := fromDeclaration(ws, a.Declaration)
		if err != nil {
			return nil, err
		}
		a.Content = content
	}
	did := "created"
	if exists {
		did = "replaced"
	}
	content := a.Content
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return &refactor.Plan{
		Summary:  fmt.Sprintf("%s %s (%d lines)", did, ws.Rel(path), strings.Count(content, "\n")),
		Edits:    []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{End: len(old), NewText: content}}, Verbatim: true}},
		Supplied: content,
	}, nil
}
