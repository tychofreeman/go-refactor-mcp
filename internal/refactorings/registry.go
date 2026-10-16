// Package refactorings is the registry of available refactorings. The CLI
// and the MCP server expose exactly this list as tools.
package refactorings

import (
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/extractfunc"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/extractvar"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/inlinevar"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/rename"
)

// All returns every available refactoring.
func All() []refactor.Refactoring {
	return []refactor.Refactoring{
		rename.New(),
		extractvar.New(),
		inlinevar.New(),
		extractfunc.New(),
		// Next: invert_if, ...
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
