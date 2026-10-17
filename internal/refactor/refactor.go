// Package refactor defines the Refactoring interface and the engine that
// runs one refactoring step safely: check preconditions, apply, re-type-check,
// and roll back if anything broke.
//
// An Edit is a step that may change behavior (creating a file, changing an
// expression). It is held to the same rule: the code type-checks before
// and after every step.
//
// Every refactoring is exposed to the model as its own tool. The model picks
// one, the engine applies it, and the returned Result (diff + status) is what
// the model looks at before choosing the next step.
package refactor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// Refactoring is one code transformation: behavior-preserving, unless it
// is also an Edit.
type Refactoring interface {
	// Name is the tool name, e.g. "rename".
	Name() string
	// Description tells the model what the refactoring does and when to
	// use it. It becomes the MCP tool description.
	Description() string
	// InputSchema is a JSON Schema object for the arguments.
	InputSchema() json.RawMessage
	// Plan checks preconditions and computes the edits. It must not write
	// anything. A precondition failure is returned as a *Refusal.
	Plan(ws *workspace.Workspace, args json.RawMessage) (*Plan, error)
}

// Edit marks a Refactoring that may change behavior, as the step that adds
// or changes a feature does. Like any step, it must leave code that
// type-checks; when it would not, the errors come from the code it was
// given, so the refusal lists them rather than reporting a bug.
type Edit interface {
	Refactoring
	ChangesBehavior()
}

// Plan is the outcome of a successful precondition check.
type Plan struct {
	Summary string          // one line, e.g. `renamed "x" to "count" (4 references)`
	Edits   []edit.FileEdit // byte-range edits against current file contents
	Notes   []string        // caveats the model should know about
}

// Refusal is a precondition failure. Its message is written for the model:
// it should say what is wrong and, where possible, what to do instead.
type Refusal struct{ Reason string }

func (r *Refusal) Error() string { return r.Reason }

// Refusef returns a *Refusal with a formatted reason.
func Refusef(format string, args ...any) error {
	return &Refusal{Reason: fmt.Sprintf(format, args...)}
}

// IsRefusal reports whether err is (or wraps) a *Refusal.
func IsRefusal(err error) bool {
	var r *Refusal
	return errors.As(err, &r)
}

// Result is returned to the caller (CLI or model) after each step.
type Result struct {
	Refactoring string   `json:"refactoring"`
	Applied     bool     `json:"applied"`
	DryRun      bool     `json:"dry_run,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Refused     string   `json:"refused,omitempty"`
	Notes       []string `json:"notes,omitempty"`
	Files       []string `json:"files,omitempty"`
	Diff        string   `json:"diff,omitempty"`
}

// String renders the result as plain text, the format sent to models.
func (r *Result) String() string {
	var sb strings.Builder
	switch {
	case r.Refused != "":
		fmt.Fprintf(&sb, "REFUSED %s: %s\n", r.Refactoring, r.Refused)
	case r.DryRun:
		fmt.Fprintf(&sb, "PREVIEW %s: %s (not applied)\n", r.Refactoring, r.Summary)
	default:
		fmt.Fprintf(&sb, "APPLIED %s: %s\n", r.Refactoring, r.Summary)
	}
	for _, n := range r.Notes {
		fmt.Fprintf(&sb, "note: %s\n", n)
	}
	if r.Diff != "" {
		sb.WriteString("\n")
		sb.WriteString(r.Diff)
	}
	return sb.String()
}

// Options controls Run.
type Options struct {
	DryRun bool // compute and show the diff without writing
}

// Run executes one refactoring step.
//
// Preconditions the engine enforces for every refactoring:
//   - the workspace type-checks before the step (refactoring broken code
//     cannot be verified);
//   - the step introduces no new errors; if it does, it is undone and
//     reported as a refusal.
//
// A refusal is a normal outcome, not an error: it is returned in Result so
// the model can read it and choose something else. The error return is for
// internal failures (I/O, loader crashes).
func Run(ws *workspace.Workspace, r Refactoring, args json.RawMessage, opts Options) (*Result, error) {
	res := &Result{Refactoring: r.Name(), DryRun: opts.DryRun}

	if errs := ws.Errors(); len(errs) > 0 {
		res.Refused = fmt.Sprintf("the code does not type-check, so refactorings cannot be verified. Fix these first:\n  %s",
			strings.Join(limit(errs, 10), "\n  "))
		return res, nil
	}

	plan, err := r.Plan(ws, args)
	if err != nil {
		if IsRefusal(err) {
			res.Refused = err.Error()
			return res, nil
		}
		return nil, err
	}
	res.Summary, res.Notes = plan.Summary, plan.Notes

	if opts.DryRun {
		c, err := ws.Preview(r.Name(), plan.Edits)
		if err != nil {
			return nil, err
		}
		res.Diff, res.Files = c.Diff, c.Files(ws.Root)
		return res, nil
	}

	c, err := ws.Apply(r.Name()+": "+plan.Summary, plan.Edits)
	if err != nil {
		return nil, err
	}
	if errs := ws.Errors(); len(errs) > 0 {
		if _, uerr := ws.Undo(); uerr != nil {
			return nil, fmt.Errorf("refactoring broke the build and undo failed: %v (errors: %v)", uerr, errs)
		}
		res.Refused = fmt.Sprintf("the change would not compile, so it was rolled back. This is a bug in the %q precondition checks; errors were:\n  %s",
			r.Name(), strings.Join(limit(errs, 10), "\n  "))
		if _, isEdit := r.(Edit); isEdit {
			res.Refused = fmt.Sprintf("the result does not type-check, so it was not applied; change the code you gave so that it compiles. Errors were:\n  %s",
				strings.Join(limit(errs, 10), "\n  "))
		}
		res.Summary = ""
		return res, nil
	}
	res.Applied = true
	res.Diff, res.Files = c.Diff, c.Files(ws.Root)
	return res, nil
}

func limit(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return append(s[:n:n], fmt.Sprintf("... and %d more", len(s)-n))
}

// DecodeArgs unmarshals args into v, rejecting unknown fields so a model
// that misspells a parameter gets told instead of silently ignored.
func DecodeArgs(args json.RawMessage, v any) error {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	dec := json.NewDecoder(strings.NewReader(string(args)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return Refusef("invalid arguments: %v", err)
	}
	return nil
}
