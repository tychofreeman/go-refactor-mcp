// Package refactorings is the registry of available refactorings and
// edits. The CLI and the MCP server expose exactly this list as tools.
package refactorings

import (
	"encoding/json"
	"fmt"
	"strings"

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
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
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
		changesig.NewChangeParameterType(),
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

// Declarer is create_from_usage, which a batch runs to declare what its
// steps call but nothing declares.
func Declarer() refactor.Refactoring {
	for _, r := range All() {
		if r.Name() == "create_from_usage" {
			return r
		}
	}
	return nil
}

// RunStep runs one refactoring or edit as the tools do: an edit whose
// result does not type-check is run again as a batch, which declares the
// functions it calls. It returns the result to show and whether the step
// was refused. A dry run is not retried, since a batch always writes.
func RunStep(ws *workspace.Workspace, r refactor.Refactoring, args json.RawMessage, opts refactor.Options) (string, bool, error) {
	res, err := refactor.Run(ws, r, args, opts)
	if err != nil {
		return "", false, err
	}
	if _, edit := r.(refactor.Edit); edit && !opts.DryRun && strings.HasPrefix(res.Refused, "the result does not type-check") {
		opts.Unchecked, opts.Declare = true, Declarer()
		b, err := refactor.RunBatch(ws, []refactor.Step{{Refactoring: r, Args: args}}, opts)
		if err != nil {
			return "", false, err
		}
		if b.Applied {
			return b.String(), false, nil
		}
		if b.FailedStep == 0 && len(b.Steps) > 1 {
			res.Refused = fmt.Sprintf("the result does not type-check, so it was not applied; change the code you gave so that it compiles. With stubs for the functions it calls, the errors left were:\n  %s", strings.Join(b.Errors, "\n  "))
		}
		res.Notes = append(res.Notes, b.Notes...)
	}
	return res.String(), res.Refused != "", nil
}

// Batch is a batch of steps as the batch tool takes it.
type Batch struct {
	Steps     []refactor.Step
	Unchecked bool // type-check only at the end ("check": "end")
	Summary   bool // report without the diff ("report": "summary")
}

// ParseBatch decodes the batch tool's arguments. When they are refused,
// it returns the message to show instead.
func ParseBatch(args json.RawMessage) (Batch, string) {
	var a struct {
		Steps []struct {
			ID   string          `json:"id"`
			Tool string          `json:"tool"`
			Args json.RawMessage `json:"args"`
		} `json:"steps"`
		Report string `json:"report"`
		Check  string `json:"check"`
	}
	if err := refactor.DecodeArgs(args, &a); err != nil {
		return Batch{}, "REFUSED batch: " + err.Error() + "\nbatch takes:\n  steps (required): the steps, in order, each {\"tool\": ..., \"args\": {...}}; a tool's own arguments (file, name, line, ...) go in each step's args\n  report: diff (the default) or summary"
	}
	if a.Check != "" && a.Check != "each" && a.Check != "end" {
		return Batch{}, fmt.Sprintf("REFUSED batch: check is %q; give each or end", a.Check)
	}
	if a.Report != "" && a.Report != "diff" && a.Report != "summary" {
		return Batch{}, fmt.Sprintf("REFUSED batch: report is %q; give diff or summary", a.Report)
	}
	if len(a.Steps) == 0 {
		return Batch{}, "REFUSED batch: steps is empty; give the refactorings and edits to run, in order"
	}
	b := Batch{Steps: make([]refactor.Step, len(a.Steps)), Unchecked: a.Check == "end", Summary: a.Report == "summary"}
	for i, st := range a.Steps {
		r, ok := Lookup(st.Tool)
		if !ok {
			return Batch{}, fmt.Sprintf("REFUSED batch: step %d: %q is not a refactoring or edit; a batch runs tools such as rename, add_parameter or replace_statements, not inspect, undo, check or batch. Nothing changed.", i+1, st.Tool)
		}
		b.Steps[i] = refactor.Step{ID: st.ID, Refactoring: r, Args: st.Args}
	}
	return b, ""
}
