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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
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
	Summary   string          // one line, e.g. `renamed "x" to "count" (4 references)`
	Edits     []edit.FileEdit // byte-range edits against current file contents
	Notes     []string        // caveats the model should know about
	Supplied  string          // code the model wrote; the result names where it went instead of repeating it
	Locations bool            // the change is the same at every site (a rename): a compact result lists where, not the diff
	Choices   []string        // what the tool decided for the model (a signature, a file), for it to check
}

// Fix is an edit that may start from code that does not type-check, such
// as a call of a function not declared yet. It must leave fewer errors,
// and no new one.
type Fix interface {
	Edit
	Fixes()
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
	Choices     []string `json:"choices,omitempty"`
	Files       []string `json:"files,omitempty"`
	Diff        string   `json:"diff,omitempty"`
	supplied    string   // the plan's Supplied, for a batch's combined diff
	locations   bool     // the plan's Locations, likewise
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
	for _, c := range r.Choices {
		fmt.Fprintf(&sb, "chose: %s\n", c)
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
	DryRun    bool // compute and show the diff without writing
	Compact   bool // show the diff without context lines: what changed and where, at less cost to read
	Unchecked bool // an edit may start from and leave code that does not type-check: a batch checked at its end
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

	before := ws.Errors()
	_, isEdit := r.(Edit)
	_, isFix := r.(Fix)
	if len(before) > 0 && !isFix && !(opts.Unchecked && isEdit) {
		res.Refused = fmt.Sprintf("the code does not type-check, so refactorings cannot be verified. Fix these first:\n  %s",
			strings.Join(limit(before, 10), "\n  "))
		if opts.Unchecked {
			res.Refused = fmt.Sprintf("the code does not type-check here, after an earlier edit of this batch, and a refactoring runs only on code that type-checks; put it before that edit, or after the step that completes it. Errors were:\n  %s",
				strings.Join(limit(before, 10), "\n  "))
		}
		return res, nil
	}

	plan, err := r.Plan(ws, args)
	if err != nil {
		if IsRefusal(err) {
			res.Refused = err.Error()
			if strings.HasPrefix(res.Refused, "invalid arguments") {
				res.Refused += argsHelp(r.Name(), r.InputSchema())
			}
			return res, nil
		}
		return nil, err
	}
	res.Summary, res.Notes = plan.Summary, plan.Notes
	res.supplied, res.locations = plan.Supplied, plan.Locations
	res.Choices = plan.Choices

	if opts.DryRun {
		c, err := ws.Preview(r.Name(), plan.Edits)
		if err != nil {
			return nil, err
		}
		res.Diff, res.Files = edit.ElideSupplied(diffOf(ws, c, opts, plan), plan.Supplied), c.Files(ws.Root)
		return res, nil
	}

	c, err := ws.Apply(r.Name()+": "+plan.Summary, plan.Edits)
	if err != nil {
		return nil, err
	}
	if reason := afterRefusal(r.Name(), isEdit, isFix, opts.Unchecked, before, ws.Errors()); reason != "" {
		if _, uerr := ws.Undo(); uerr != nil {
			return nil, fmt.Errorf("the step broke the build and undo failed: %v (%s)", uerr, reason)
		}
		res.Refused, res.Summary = reason, ""
		return res, nil
	}
	res.Applied = true
	res.Diff, res.Files = edit.ElideSupplied(diffOf(ws, c, opts, plan), plan.Supplied), c.Files(ws.Root)
	return res, nil
}

// afterRefusal says why the step named name, which left the errors after
// from before, is refused, or "" if it is not.
func afterRefusal(name string, isEdit, isFix, unchecked bool, before, after []string) string {
	switch {
	case len(after) == 0 || unchecked && isEdit:
		return ""
	case isFix:
		return fixRefusal(before, after)
	case isEdit:
		hint := ""
		if slices.ContainsFunc(after, func(e string) bool { return strings.Contains(e, "undefined: ") }) {
			hint = "\nIf it calls functions that do not exist yet, send it in a batch with check \"end\", then create_from_usage to declare them."
		}
		return fmt.Sprintf("the result does not type-check, so it was not applied; change the code you gave so that it compiles. Errors were:\n  %s%s",
			strings.Join(limit(after, 10), "\n  "), hint)
	}
	return fmt.Sprintf("the change would not compile, so it was rolled back. This is a bug in the %q precondition checks; errors were:\n  %s",
		name, strings.Join(limit(after, 10), "\n  "))
}

// fixRefusal says why a fix that left the errors after, from before, is
// refused, or "" if it is not: it must fix some and add none.
func fixRefusal(before, after []string) string {
	had := map[string]int{}
	for _, e := range before {
		had[withoutPosition(e)]++
	}
	var added []string
	for _, e := range after {
		k := withoutPosition(e)
		if had[k] == 0 {
			added = append(added, e)
			continue
		}
		had[k]--
	}
	switch {
	case len(added) > 0:
		return fmt.Sprintf("the change adds errors the code did not have, so it was not applied. New errors:\n  %s", strings.Join(limit(added, 10), "\n  "))
	case len(after) >= len(before):
		return fmt.Sprintf("the change fixes none of the errors, so it was not applied. Errors are:\n  %s", strings.Join(limit(after, 10), "\n  "))
	}
	return ""
}

// position is the file:line:col: prefix of an error, which an edit above
// it moves.
var position = regexp.MustCompile(`^\S*:\d+:\d+: `)

func withoutPosition(e string) string { return position.ReplaceAllString(e, "") }

// diffOf is c's diff as the result shows it.
func diffOf(ws *workspace.Workspace, c workspace.Change, opts Options, plan *Plan) string {
	if opts.Compact && plan.Locations {
		return edit.Locations(c.DiffContext(ws.Root, 0))
	}
	if opts.Compact {
		return c.DiffContext(ws.Root, 0)
	}
	return c.Diff
}

// Step is one refactoring or edit of a batch, with its arguments.
type Step struct {
	Refactoring Refactoring
	Args        json.RawMessage
}

// BatchResult is the outcome of RunBatch.
type BatchResult struct {
	Applied    bool
	Steps      []*Result // the steps run, in order; when refused, the last one is the one refused
	Total      int       // how many steps the batch had
	FailedStep int       // 1-based, when refused
	FailedTool string    // the refused step's tool
	Refused    string
	Diff       string // of the whole batch, when applied
	Files      []string
}

// RunBatch runs steps in order, each through Run, as one change: if a
// step is refused, the ones before it are undone and the batch is refused;
// if all apply, they become one entry on the undo stack.
//
// Line numbers in the steps (line, start_line, end_line) are those of the
// files as they were when the batch began, as the model last inspected
// them: each is moved to where that line is after the earlier steps. A
// step that addresses a line an earlier step rewrote or removed, or a
// line of a file created in the batch, is refused.
func RunBatch(ws *workspace.Workspace, steps []Step, opts Options) (*BatchResult, error) {
	b := &BatchResult{Total: len(steps)}
	lines := map[string]*lineMap{} // by file, relative to the root: its lines through the batch
	applied := 0
	rollback := func() error {
		for ; applied > 0; applied-- {
			if _, err := ws.Undo(); err != nil {
				return fmt.Errorf("rolling back the batch: %w", err)
			}
		}
		return nil
	}
	var names []string
	for i, s := range steps {
		name := s.Refactoring.Name()
		args := s.Args
		if file, byLine := lineAddressed(s.Args); byLine {
			rel := ws.Rel(ws.Abs(file))
			if m := lines[rel]; m != nil {
				translated, err := translateLines(s.Args, rel, m)
				if err != nil {
					b.FailedStep, b.FailedTool, b.Refused = i+1, name, err.Error()
					return b, rollback()
				}
				args = translated
			}
		}
		res, err := Run(ws, s.Refactoring, args, Options{Compact: opts.Compact, Unchecked: opts.Unchecked})
		if err != nil {
			if rerr := rollback(); rerr != nil {
				return nil, fmt.Errorf("step %d (%s): %v; %v", i+1, name, err, rerr)
			}
			return nil, fmt.Errorf("step %d (%s): %w", i+1, name, err)
		}
		b.Steps = append(b.Steps, res)
		if !res.Applied {
			b.FailedStep, b.FailedTool, b.Refused = i+1, name, res.Refused
			return b, rollback()
		}
		applied++
		names = append(names, name)
		if c, ok := ws.LastChange(); ok {
			for p, before := range c.Before {
				rel := ws.Rel(p)
				if lines[rel] == nil {
					lines[rel] = newLineMap(before)
				}
				lines[rel].follow(i+1, before, c.After[p])
			}
		}
	}
	if opts.Unchecked {
		if errs := ws.Errors(); len(errs) > 0 {
			b.Refused = fmt.Sprintf("after all %d step%s the code does not type-check, so the batch was rolled back. Errors were:\n  %s",
				len(steps), textutil.Plural(len(steps)), strings.Join(limit(errs, 10), "\n  "))
			return b, rollback()
		}
	}
	b.Applied = true
	if applied == 0 {
		return b, nil
	}
	c, err := ws.Squash(applied, "batch: "+strings.Join(names, ", "))
	if err != nil {
		return nil, err
	}
	b.Files = c.Files(ws.Root)
	b.Diff = c.Diff
	locations := true
	var supplied []string
	for _, r := range b.Steps {
		locations = locations && r.locations
		if r.supplied != "" {
			supplied = append(supplied, r.supplied)
		}
	}
	if opts.Compact {
		b.Diff = c.DiffContext(ws.Root, 0)
		if locations {
			b.Diff = edit.Locations(b.Diff)
		}
	}
	b.Diff = edit.ElideSupplied(b.Diff, strings.Join(supplied, "\n"))
	return b, nil
}

// lineMap follows a file's lines through the steps of a batch. For each
// line (1-based) of the file as it was when the batch began, now is the
// line it is on now, or 0 once a step rewrote or removed it; changed is
// that step.
type lineMap struct {
	now     []int
	changed []int
	created bool // the file did not exist when the batch began
}

func newLineMap(before []byte) *lineMap {
	now := edit.LineMap(before, before)
	return &lineMap{now: now, changed: make([]int, len(now)), created: before == nil}
}

// follow moves m's lines through step, which changed the file from before
// to after.
func (m *lineMap) follow(step int, before, after []byte) {
	moved := edit.LineMap(before, after)
	for i, cur := range m.now {
		if cur == 0 {
			continue
		}
		if cur >= len(moved) || moved[cur] == 0 {
			m.now[i] = 0
			if m.changed[i] == 0 {
				m.changed[i] = step
			}
			continue
		}
		m.now[i] = moved[cur]
	}
}

// translateLines rewrites a step's line arguments (line, start_line,
// end_line), given as of the start of the batch, to where those lines are
// now. A line an earlier step rewrote or removed cannot be addressed.
func translateLines(args json.RawMessage, rel string, m *lineMap) (json.RawMessage, error) {
	var a map[string]json.RawMessage
	if err := json.Unmarshal(args, &a); err != nil {
		return args, nil
	}
	for _, key := range []string{"line", "start_line", "end_line"} {
		var n int
		if raw, ok := a[key]; !ok || json.Unmarshal(raw, &n) != nil || n <= 0 {
			continue
		}
		switch {
		case m.created:
			return nil, fmt.Errorf("%s was created in this batch, so it has no line %d from before it; address what is in it by name", rel, n)
		case n >= len(m.now):
			return nil, fmt.Errorf("%s had %d lines when the batch began, so there is no line %d", rel, len(m.now)-1, n)
		case m.now[n] == 0:
			return nil, fmt.Errorf("line %d of %s (as it was when the batch began) was rewritten by step %d, so it is not there to address; address what step %d made by name, or send this step in a later call with line numbers from a new inspect", n, rel, m.changed[n], m.changed[n])
		}
		a[key], _ = json.Marshal(m.now[n])
	}
	return json.Marshal(a)
}

// String renders the batch result as plain text, the format sent to models.
func (b *BatchResult) String() string {
	var sb strings.Builder
	if !b.Applied && b.FailedStep == 0 {
		fmt.Fprintf(&sb, "REFUSED batch: %s\nNothing changed.\n", b.Refused)
		return sb.String()
	}
	if !b.Applied {
		fmt.Fprintf(&sb, "REFUSED batch at step %d of %d (%s): %s\n", b.FailedStep, b.Total, b.FailedTool, b.Refused)
		switch n := b.FailedStep - 1; n {
		case 0:
			sb.WriteString("Nothing changed.\n")
		case 1:
			sb.WriteString("The earlier step 1 was rolled back; nothing changed.\n")
		default:
			fmt.Fprintf(&sb, "Steps 1-%d were rolled back; nothing changed.\n", n)
		}
		return sb.String()
	}
	fmt.Fprintf(&sb, "APPLIED batch: %d step%s\n", len(b.Steps), textutil.Plural(len(b.Steps)))
	for i, r := range b.Steps {
		fmt.Fprintf(&sb, "%d. %s: %s\n", i+1, r.Refactoring, r.Summary)
		for _, c := range r.Choices {
			fmt.Fprintf(&sb, "   chose: %s\n", c)
		}
		for _, n := range r.Notes {
			fmt.Fprintf(&sb, "   note: %s\n", n)
		}
	}
	if b.Diff != "" {
		sb.WriteString("\n")
		sb.WriteString(b.Diff)
	}
	return sb.String()
}

// lineAddressed returns the file a step's arguments address by line, if
// they do.
func lineAddressed(args json.RawMessage) (string, bool) {
	var a struct {
		File      string `json:"file"`
		Line      int    `json:"line"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
		Column    int    `json:"column"`
	}
	if json.Unmarshal(args, &a) != nil || a.File == "" {
		return "", false
	}
	return a.File, a.Line != 0 || a.StartLine != 0 || a.EndLine != 0 || a.Column != 0
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

// argsHelp lists a tool's arguments, from its input schema and in its
// order, for a refusal of arguments it does not take.
func argsHelp(name string, schema json.RawMessage) string {
	var s struct {
		Properties json.RawMessage `json:"properties"`
		Required   []string        `json:"required"`
	}
	if json.Unmarshal(schema, &s) != nil || len(s.Properties) == 0 {
		return ""
	}
	// Decode the properties token by token to keep the schema's order.
	dec := json.NewDecoder(bytes.NewReader(s.Properties))
	if _, err := dec.Token(); err != nil {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n%s takes:", name)
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		key, _ := tok.(string)
		var p struct {
			Description string `json:"description"`
		}
		if err := dec.Decode(&p); err != nil {
			return ""
		}
		required := ""
		if slices.Contains(s.Required, key) {
			required = " (required)"
		}
		fmt.Fprintf(&sb, "\n  %s%s: %s", key, required, p.Description)
	}
	return sb.String()
}
