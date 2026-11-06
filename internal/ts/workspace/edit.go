package workspace

import (
	"os"
	"slices"

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
