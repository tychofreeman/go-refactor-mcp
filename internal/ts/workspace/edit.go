package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
)

// project is the first project that has the file at the absolute path, or
// nil.
func (w *Workspace) project(path string) *tsapi.Project {
	for _, p := range w.projects {
		if p.Has(path) {
			return p
		}
	}
	return nil
}

func (w *Workspace) none(path string) string {
	return w.Rel(path) + " is in none of the solution's projects (" + joinComma(w.Projects()) + ")"
}

// Project returns the absolute path of file and the first project of the
// solution that has it; refusal is why there is none: no such file, or
// it is in none of the projects.
func (w *Workspace) Project(file string) (path string, p *tsapi.Project, refusal string) {
	path = w.Abs(file)
	if _, err := os.Stat(path); err != nil {
		return path, nil, file + ": no such file"
	}
	if p = w.project(path); p == nil {
		return path, nil, w.none(path)
	}
	return path, p, ""
}

// ClassMembers returns tsapi's ClassMembers from the first project of the
// solution that declares every name, or why none does.
func (w *Workspace) ClassMembers(implements []string, base string) ([]string, string) {
	for _, p := range w.projects {
		if members, found, why := p.ClassMembers(implements, base); found {
			return members, why
		}
	}
	names := append(slices.Clone(implements), base)
	return nil, "no project of the solution declares all of " + joinComma(slices.DeleteFunc(names, func(s string) bool { return s == "" })) + " as a top-level interface or class"
}

// TestFor returns tsapi's TestFor from the first project that declares
// target, and the module to import test from: vitest or @jest/globals when
// the root's package.json names one, "" for jest's globals, else node:test.
func (w *Workspace) TestFor(target string) (test, module, refusal string) {
	for _, p := range w.projects {
		if test, found, why := p.TestFor(context.Background(), target); found {
			return test, w.testModule(), why
		}
	}
	return "", "", "no project of the solution has a top-level function or class " + strings.SplitN(target, ".", 2)[0] + "; test_for names a function or Class.method"
}

// testModule is the module the solution's tests import test from.
func (w *Workspace) testModule() string {
	var pkg struct {
		Dependencies, DevDependencies map[string]string
	}
	if b, err := os.ReadFile(filepath.Join(w.Root, "package.json")); err == nil {
		json.Unmarshal(b, &pkg)
	}
	has := func(name string) bool {
		_, dep := pkg.Dependencies[name]
		_, dev := pkg.DevDependencies[name]
		return dep || dev
	}
	switch {
	case has("vitest"):
		return "vitest"
	case has("@jest/globals"):
		return "@jest/globals"
	case has("jest"):
		return ""
	}
	return "node:test"
}

// Construct returns edits of the file at path, whose text is before, with
// the constructors tsapi.WriteConstructors writes for the classes they
// leave with fields to set, as one edit of the whole file, and its notes;
// edits as they are when the file's project does not need fields set or
// no class does.
func (w *Workspace) Construct(path, before string, edits []edit.TextEdit) ([]edit.TextEdit, []string) {
	p := w.project(path)
	if p == nil || !p.StrictPropertyInitialization() {
		return edits, nil
	}
	after, err := edit.Apply([]byte(before), edits)
	if err != nil {
		return edits, nil
	}
	written, notes := tsapi.WriteConstructors(before, string(after))
	if len(notes) == 0 {
		return edits, nil
	}
	return []edit.TextEdit{{Start: 0, End: len(before), NewText: written}}, notes
}
