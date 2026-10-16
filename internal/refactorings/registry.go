// Package refactorings is the registry of available refactorings. The CLI
// and (later) the MCP server expose exactly this list as tools.
package refactorings

import (
	"github.com/cwfreeman/gorefactor/internal/refactor"
	"github.com/cwfreeman/gorefactor/internal/refactorings/extractvar"
	"github.com/cwfreeman/gorefactor/internal/refactorings/rename"
)

// All returns every available refactoring.
func All() []refactor.Refactoring {
	return []refactor.Refactoring{
		rename.New(),
		extractvar.New(),
		// Next: inline_variable, invert_if, extract_function, ...
	}
}

// Lookup returns the refactoring with the given name.
func Lookup(name string) (refactor.Refactoring, bool) {
	for _, r := range All() {
		if r.Name() == name {
			return r, true
		}
	}
	return nil, false
}
