// Package refactorings is the registry of available refactorings and
// edits. The CLI and the MCP server expose exactly this list as tools.
package refactorings

import (
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/addcase"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/adddecl"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/addelement"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/addfield"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/createfile"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/createfromusage"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/insertstmts"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/replaceexpr"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/replacestmts"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/replacetext"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/changesig"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/deadcode"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/extractfunc"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/extractstruct"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/extractvar"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/inlinefunc"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/inlinevar"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/invertif"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/mergefunc"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/movedecl"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/rename"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/slidestmt"
)

// All returns every available refactoring and edit.
func All() []refactor.Refactoring {
	return []refactor.Refactoring{
		rename.New(),
		extractvar.New(),
		inlinevar.New(),
		extractfunc.New(),
		inlinefunc.New(),
		invertif.New(),
		invertif.NewRemoveElse(),
		changesig.NewAddParameter(),
		changesig.NewRemoveParameter(),
		changesig.NewMoveParameter(),
		changesig.NewIntroduceParameter(),
		changesig.NewMethodToFunction(),
		changesig.NewFunctionToMethod(),
		movedecl.New(),
		extractstruct.New(),
		mergefunc.New(),
		deadcode.New(),
		slidestmt.NewUp(),
		slidestmt.NewDown(),
		createfile.New(),
		replacestmts.New(),
		insertstmts.New(),
		replaceexpr.New(),
		adddecl.New(),
		addfield.New(),
		replacetext.New(),
		addcase.New(),
		addelement.New(),
		createfromusage.New(),
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
