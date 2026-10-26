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
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
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

// APINote is the note for a change to name, which is part of the
// module's API; change says what was done to it ("removing it").
func APINote(name, change string) string {
	return fmt.Sprintf("%s is exported: %s changes the module's API; code in other modules that uses it is not updated and breaks, so a release needs a new major version", name, change)
}

// Fix is an edit that may start from code that does not type-check, such
// as a call of a function not declared yet. It must leave fewer errors,
// and no new one.
type Fix interface {
	Edit
	Fixes()
}

// BreakageNoter is an edit that may leave code that does not type-check
// for later steps of a batch to fix (change_type): in a batch checked at
// its end, its result lists the errors it left.
type BreakageNoter interface {
	Edit
	NotesBreakage()
}

// CompileChecked is a refactoring whose last precondition is that the
// code compiles after it (for example, that a body still type-checks
// with a parameter's new type): a compile error is a refusal, not a bug.
type CompileChecked interface {
	Refactoring
	ChecksByCompiling()
}

// Expander is a refactoring that, for some arguments, is a sequence of
// its own steps (inline_function with all_calls: one step per call).
// Expand returns them, which Run runs as one batch, or nil to plan the
// step as usual.
type Expander interface {
	Refactoring
	Expand(ws *workspace.Workspace, args json.RawMessage) ([]Step, error)
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
	Refactoring string          `json:"refactoring"`
	Applied     bool            `json:"applied"`
	DryRun      bool            `json:"dry_run,omitempty"`
	Summary     string          `json:"summary,omitempty"`
	Refused     string          `json:"refused,omitempty"`
	Notes       []string        `json:"notes,omitempty"`
	Choices     []string        `json:"choices,omitempty"`
	Files       []string        `json:"files,omitempty"`
	Diff        string          `json:"diff,omitempty"`
	supplied    string          // the plan's Supplied, for a batch's combined diff
	locations   bool            // the plan's Locations, likewise
	edits       []edit.FileEdit // the plan's Edits, for a batch to tell where the step changed each file
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
	DryRun    bool                                            // compute and show the diff without writing
	Compact   bool                                            // show the diff without context lines: what changed and where, at less cost to read
	Unchecked bool                                            // an edit may start from and leave code that does not type-check: a batch checked at its end
	Declare   Refactoring                                     // with Unchecked, in a batch: declares what the steps call but nothing declares (create_from_usage)
	Imports   Refactoring                                     // with Unchecked, in a batch: adds the imports the steps' code needs and removes those it leaves unused (fix_imports)
	Test      func(pattern string) (output string, err error) // a checkpoint's tests; GoTest if nil
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
	// An edit may start from code that does not type-check: then, like a
	// fix, it must leave fewer errors (see afterRefusal).
	// A refactoring may run on code whose only errors are in tests written
	// first against what it makes (a test calling the name a rename
	// gives): like a fix, it must then fix some of them and add none.
	towardTests := len(before) > 0 && !isFix && !isEdit && inTestsOnly(before)
	if len(before) > 0 && !isFix && !isEdit && !towardTests {
		res.Refused = fmt.Sprintf("the code does not type-check, so refactorings cannot be verified. Fix these first (an edit, such as add_field for a field a test already uses, may run on code that does not type-check if it fixes some of the errors):\n  %s",
			strings.Join(limit(before, 10), "\n  "))
		if opts.Unchecked {
			res.Refused = fmt.Sprintf("the code does not type-check here, after an earlier edit of this batch, and a refactoring runs only on code that type-checks; put it before that edit, or after the step that completes it. Errors were:\n  %s",
				strings.Join(limit(before, 10), "\n  "))
		}
		return res, nil
	}

	args, took := aliasArgs(r.InputSchema(), args)
	if ex, ok := r.(Expander); ok {
		steps, err := ex.Expand(ws, args)
		if err != nil && IsRefusal(err) {
			res.Refused = err.Error()
			return res, nil
		}
		if err != nil {
			return nil, err
		}
		if steps != nil {
			return runExpanded(ws, steps, opts, res, took)
		}
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
	res.supplied, res.locations, res.edits = plan.Supplied, plan.Locations, plan.Edits
	res.Choices = plan.Choices
	if took != "" {
		res.Notes = append(res.Notes, took)
	}

	if opts.DryRun {
		c, err := ws.Preview(r.Name(), plan.Edits)
		if err != nil {
			return nil, err
		}
		res.Diff, res.Files = edit.ElideSupplied(diffOf(ws, c, opts, plan), plan.Supplied), c.Files(ws.Root)
		return res, nil
	}

	sigNote := ""
	if isEdit {
		sigNote = signatureNote(ws, plan.Edits)
	}
	c, err := ws.Apply(r.Name()+": "+plan.Summary, plan.Edits)
	if errors.Is(err, workspace.ErrBreaksLoading) {
		res.Refused, res.Summary = err.Error(), ""
		return res, nil
	}
	if err != nil {
		return nil, err
	}
	if reason := afterRefusal(r, isEdit, isFix || towardTests, opts.Unchecked, before, ws.Errors()); reason != "" {
		if _, uerr := ws.Undo(); uerr != nil {
			return nil, fmt.Errorf("the step broke the build and undo failed: %v (%s)", uerr, reason)
		}
		if sigNote != "" {
			reason += "\n" + sigNote
		}
		res.Refused, res.Summary = reason, ""
		return res, nil
	}
	res.Applied = true
	res.Diff, res.Files = edit.ElideSupplied(diffOf(ws, c, opts, plan), plan.Supplied), c.Files(ws.Root)
	if _, notes := r.(BreakageNoter); notes && opts.Unchecked {
		if left := newErrors(before, ws.Errors()); len(left) > 0 {
			res.Notes = append(res.Notes, fmt.Sprintf("left %d type error%s for the next steps of this batch to fix (it is checked at its end):\n  %s",
				len(left), textutil.Plural(len(left)), strings.Join(limit(left, 20), "\n  ")))
		}
	}
	return res, nil
}

// runExpanded runs the steps a step expanded to as one batch, and reports
// them in res as that step: applied as a whole, or refused as a whole.
func runExpanded(ws *workspace.Workspace, steps []Step, opts Options, res *Result, took string) (*Result, error) {
	b, err := RunBatch(ws, steps, Options{Compact: opts.Compact, Unchecked: opts.Unchecked})
	if err != nil {
		return nil, err
	}
	if !b.Applied {
		res.Refused = fmt.Sprintf("this is %d steps of %s, run one after another; step %d%s was refused, so none was applied: %s",
			b.Total, res.Refactoring, b.FailedStep, stepPlace(steps[b.FailedStep-1].Args), b.Refused)
		return res, nil
	}
	var summaries []string
	seen := map[string]bool{}
	for _, s := range b.Steps {
		summaries = append(summaries, s.Summary)
		for _, n := range s.Notes {
			if !seen[n] {
				seen[n] = true
				res.Notes = append(res.Notes, n)
			}
		}
	}
	res.Notes = append(res.Notes, b.Notes...)
	if took != "" {
		res.Notes = append(res.Notes, took)
	}
	res.Summary = strings.Join(summaries, "; ")
	res.Diff, res.Files = b.Diff, b.Files
	if opts.DryRun {
		if _, err := ws.Undo(); err != nil {
			return nil, err
		}
		return res, nil
	}
	res.Applied = true
	return res, nil
}

// stepPlace is " (file:line)" for a step's args that give both, or "".
func stepPlace(args json.RawMessage) string {
	var a struct {
		File string `json:"file"`
		Line int    `json:"line"`
	}
	if json.Unmarshal(args, &a) != nil || a.File == "" || a.Line == 0 {
		return ""
	}
	return fmt.Sprintf(" (%s:%d)", a.File, a.Line)
}

// afterRefusal says why the step r, which left the errors after
// from before, is refused, or "" if it is not.
func afterRefusal(r Refactoring, isEdit, isFix, unchecked bool, before, after []string) string {
	_, compiles := r.(CompileChecked)
	switch {
	case len(after) == 0 || unchecked && isEdit:
		return ""
	case isFix, isEdit && len(before) > 0:
		return fixRefusal(before, after)
	case isEdit:
		return fmt.Sprintf("the result does not type-check, so it was not applied; change the code you gave so that it compiles. Errors were:\n  %s",
			strings.Join(limit(after, 10), "\n  "))
	}
	if compiles {
		return fmt.Sprintf("the change would not compile, so it was not applied. Errors were:\n  %s", strings.Join(limit(after, 10), "\n  "))
	}
	return fmt.Sprintf("the change would not compile, so it was rolled back. This is a bug in the %q precondition checks; errors were:\n  %s",
		r.Name(), strings.Join(limit(after, 10), "\n  "))
}

// inTestsOnly reports whether every error is in a test file.
func inTestsOnly(errs []string) bool {
	for _, e := range errs {
		file, _, _ := strings.Cut(e, ":")
		if !strings.HasSuffix(file, "_test.go") {
			return false
		}
	}
	return true
}

// newErrors are the errors of after that are not in before.
func newErrors(before, after []string) []string {
	var out []string
	for _, e := range after {
		if !slices.Contains(before, e) {
			out = append(out, e)
		}
	}
	return out
}

// signaturePrefix begins signatureNote's note, which a batch keeps when
// its run again with check "end" replaces the refusal it is in.
const signaturePrefix = "this edit changes the signature of"

// signatureNote is, for edits that change the signature of a function or
// interface method (its name, parameters or results), a note naming the
// tools that change one with every call: for one signature whose
// parameters alone change, the steps; "" for other edits.
func signatureNote(ws *workspace.Workspace, fes []edit.FileEdit) string {
	var names, steps []string
	for _, fe := range fes {
		_, f := ws.FileSyntax(fe.Path)
		if f == nil {
			continue
		}
		off := func(p token.Pos) int { return ws.Fset.Position(p).Offset }
		touched := func(from, to token.Pos) bool {
			for _, e := range fe.Edits {
				if e.Start < off(to) && (e.End > off(from) || e.Start == e.End && e.Start > off(from)) {
					return true
				}
			}
			return false
		}
		var after []signature
		if src, err := os.ReadFile(fe.Path); err == nil {
			if out, err := edit.Apply(src, fe.Edits); err == nil {
				if nf, err := parser.ParseFile(token.NewFileSet(), "", out, 0); err == nil {
					after = signatures(nf)
				}
			}
		}
		for _, s := range signatures(f) {
			if !touched(s.from, s.to) || slices.Contains(names, s.name) {
				continue
			}
			i := slices.IndexFunc(after, func(a signature) bool { return a.name == s.name })
			if i >= 0 && types.ExprString(after[i].typ) == types.ExprString(s.typ) {
				continue // rewritten as it was
			}
			names = append(names, s.name)
			if i >= 0 {
				steps = paramSteps(s.name, s.typ, after[i].typ)
			}
		}
	}
	switch {
	case len(names) == 0:
		return ""
	case len(names) == 1 && len(steps) > 0:
		return fmt.Sprintf(signaturePrefix+" %s, which its calls (and an interface method's implementations) do not follow as text; send instead, in one batch: %s", names[0], strings.Join(steps, "; then "))
	}
	return fmt.Sprintf(signaturePrefix+" %s; add_parameter, remove_parameter, move_parameter, change_parameter_type and add_result change a signature with every call (and an interface method with its implementations), and rename a name", orList(names, "and"))
}

// signature is a function's, method's or interface method's type, by name.
type signature struct {
	name     string // Name, Type.Method or Interface.Method
	typ      *ast.FuncType
	from, to token.Pos // what an edit of the signature touches
}

// signatures lists the functions, methods and interface methods of f.
func signatures(f *ast.File) []signature {
	var out []signature
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			name := n.Name.Name
			if n.Recv != nil && len(n.Recv.List) == 1 {
				t := n.Recv.List[0].Type
				if s, ok := t.(*ast.StarExpr); ok {
					t = s.X
				}
				switch x := t.(type) {
				case *ast.IndexExpr:
					t = x.X
				case *ast.IndexListExpr:
					t = x.X
				}
				if id, ok := t.(*ast.Ident); ok {
					name = id.Name + "." + name
				}
			}
			out = append(out, signature{name, n.Type, n.Name.Pos(), n.Type.End()})
		case *ast.TypeSpec:
			if it, ok := n.Type.(*ast.InterfaceType); ok {
				for _, m := range it.Methods.List {
					if ft, ok := m.Type.(*ast.FuncType); ok && len(m.Names) == 1 {
						out = append(out, signature{n.Name.Name + "." + m.Names[0].Name, ft, m.Pos(), m.End()})
					}
				}
			}
		}
		return true
	})
	return out
}

// paramSteps lists the steps that change the signature of name from old
// to new with every call, when only its parameters' names and types
// differ: parameters added, then the edits that use them, then those
// removed. It is nil when they cannot say (unnamed parameters, results
// or order that differ).
func paramSteps(name string, old, new *ast.FuncType) []string {
	type param struct{ name, typ string }
	params := func(fl *ast.FieldList) ([]param, bool) {
		var out []param
		if fl == nil {
			return nil, true
		}
		for _, f := range fl.List {
			if len(f.Names) == 0 {
				return nil, false
			}
			for _, n := range f.Names {
				out = append(out, param{n.Name, types.ExprString(f.Type)})
			}
		}
		return out, true
	}
	results := func(ft *ast.FuncType) string {
		return types.ExprString(&ast.FuncType{Func: token.NoPos, Params: &ast.FieldList{}, Results: ft.Results})
	}
	op, ok1 := params(old.Params)
	np, ok2 := params(new.Params)
	if !ok1 || !ok2 || results(old) != results(new) {
		return nil
	}
	index := func(ps []param, n string) int {
		return slices.IndexFunc(ps, func(p param) bool { return p.name == n })
	}
	var add, retype, remove []string
	for _, p := range np {
		switch i := index(op, p.name); {
		case i < 0:
			add = append(add, fmt.Sprintf("add_parameter of %s %s on %s (value: what each call passes)", p.name, p.typ, name))
		case op[i].typ != p.typ:
			retype = append(retype, fmt.Sprintf("change_parameter_type of %s to %s on %s", p.name, p.typ, name))
		}
	}
	for _, p := range op {
		if index(np, p.name) < 0 {
			remove = append(remove, fmt.Sprintf("remove_parameter %s on %s", p.name, name))
		}
	}
	if len(add) > 0 && len(remove) > 0 {
		add = append(add, "the edits that make the body (each implementation, for an interface method) use the new parameters in place of the old (or, for a new parameter that stands for an expression of the old ones, introduce_parameter of that expression by function on one body, in place of add_parameter and these edits: every implementation gets it)")
	}
	return slices.Concat(add, retype, remove)
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
	ID          string // optional: a name later steps can refer to it by
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
	Notes      []string // about the batch as a whole, when applied
	Errors     []string // when refused at its end: the errors left
	Kept       int      // when refused: the 1-based step of the last checkpoint that passed, through which the steps were kept; 0 if none
}

// RunBatch runs steps in order, each through Run, as one change: if a
// step is refused, the ones before it are undone and the batch is refused;
// if all apply, they become one entry on the undo stack.
//
// Line numbers in the steps (line, start_line, end_line) are those of the
// files as they were when the batch began, as the model last inspected
// them: each is moved to where that line is after the earlier steps. A
// step that addresses a line an earlier step rewrote or removed, or a
// line of a file created in the batch, is refused. A line may instead be
// a reference to the lines an earlier step made (see batchref.go).
//
// A batch checked at each step, whose edit does not type-check, runs again
// checked at its end: a later step may complete that one, or, with
// opts.Declare, the batch declares what the steps call. If that is refused
// too, the first refusal stands, with the second's.
func RunBatch(ws *workspace.Workspace, steps []Step, opts Options) (*BatchResult, error) {
	b, err := runBatch(ws, steps, opts)
	if err != nil || b.Applied || b.Kept > 0 || opts.Unchecked || !retriable(b, opts) {
		return b, err
	}
	retry, err := runBatch(ws, steps, Options{Compact: opts.Compact, Unchecked: true, Declare: opts.Declare, Imports: opts.Imports})
	if err != nil {
		return nil, err
	}
	switch {
	case !retry.Applied && retry.FailedStep == 0:
		// Every step ran: the errors left at the end are the ones to fix.
		if b.FailedStep < b.Total {
			retry.Refused = fmt.Sprintf("step %d did not type-check on its own, so the batch ran again with check \"end\"; ", b.FailedStep) + retry.Refused
		}
		if i := strings.Index(b.Refused, signaturePrefix); i >= 0 {
			line, _, _ := strings.Cut(b.Refused[i:], "\n")
			retry.Refused += "\n" + line
		}
		return retry, nil
	case !retry.Applied:
		b.Refused += fmt.Sprintf("\nRun again with check \"end\", the batch was refused at step %d (%s): %s", retry.FailedStep, retry.FailedTool, retry.Refused)
		return b, nil
	}
	if b.FailedStep < b.Total {
		retry.Notes = append(retry.Notes, fmt.Sprintf("step %d did not type-check on its own, so the batch ran again with check \"end\"", b.FailedStep))
	}
	return retry, nil
}

// retriable reports whether b, refused when checked at each step, may
// apply checked at its end: an edit that does not type-check, before the
// last step or with something to declare what it calls.
func retriable(b *BatchResult, opts Options) bool {
	return b.FailedStep > 0 && strings.HasPrefix(b.Refused, "the result does not type-check") && (b.FailedStep < b.Total || opts.Declare != nil || opts.Imports != nil)
}

// runBatch runs steps once, type-checked as opts says.
func runBatch(ws *workspace.Workspace, steps []Step, opts Options) (*BatchResult, error) {
	b := &BatchResult{Total: len(steps)}
	if err := checkIDs(steps); err != nil {
		b.Refused = err.Error()
		return b, nil
	}
	if err := checkArgs(steps); err != nil {
		b.Refused = err.Error()
		return b, nil
	}
	before := declaredNames(ws)                       // by file: what its package declared when the batch began
	lines := map[string]*lineMap{}                    // by file, relative to the root: its lines through the batch
	made := make([]map[string]*stepLines, len(steps)) // by step, then file: the lines it made
	applied := 0
	kept, keptStep, keptNames := 0, 0, 0 // at the last checkpoint that passed: changes applied, its step, names of the steps
	var names []string
	// rollback undoes the steps after the last checkpoint that passed, and
	// makes those before it one change.
	rollback := func() error {
		for ; applied > kept; applied-- {
			if _, err := ws.Undo(); err != nil {
				return fmt.Errorf("rolling back the batch: %w", err)
			}
		}
		if kept == 0 {
			return nil
		}
		b.Kept = keptStep
		c, err := ws.Squash(kept, fmt.Sprintf("batch through step %d: %s", keptStep, strings.Join(names[:keptNames], ", ")))
		if err != nil {
			return err
		}
		b.Files, b.Diff = c.Files(ws.Root), c.Diff
		if opts.Compact {
			b.Diff = c.DiffContext(ws.Root, 0)
		}
		return nil
	}
	// complete adds what the engine supplies for the steps' edits: the
	// imports their code needs, then stubs for what it calls but nothing
	// declares. Before step next (an index; len(steps) at the end), the
	// later steps' lines follow what it adds. It returns notes on the
	// names it did not declare.
	complete := func(next int) ([]string, error) {
		done := func(res *Result) {
			b.Steps = append(b.Steps, res)
			applied++
			names = append(names, res.Refactoring)
			if next < len(steps) {
				followStep(ws, made, next, lines, res)
			}
		}
		errs := ws.Errors()
		if len(errs) > 0 && opts.Imports != nil {
			_, err := fixImports(ws, errs, opts, done)
			if err != nil {
				if rerr := rollback(); rerr != nil {
					return nil, fmt.Errorf("fixing imports: %v; %v", err, rerr)
				}
				return nil, fmt.Errorf("fixing imports: %w", err)
			}
			errs = ws.Errors()
		}
		if len(errs) == 0 || opts.Declare == nil {
			return nil, nil
		}
		_, notDeclared, err := declareMissing(ws, errs, before, opts, done)
		if err != nil {
			if rerr := rollback(); rerr != nil {
				return nil, fmt.Errorf("declaring what the steps call: %v; %v", err, rerr)
			}
			return nil, fmt.Errorf("declaring what the steps call: %w", err)
		}
		return notDeclared, nil
	}
	for i, s := range steps {
		if _, ok := s.Refactoring.(*Checkpoint); ok {
			res, err := checkpoint(ws, s.Args, opts, func() error { _, err := complete(i); return err })
			if err != nil {
				if rerr := rollback(); rerr != nil {
					return nil, fmt.Errorf("step %d (check): %v; %v", i+1, err, rerr)
				}
				return nil, fmt.Errorf("step %d (check): %w", i+1, err)
			}
			b.Steps = append(b.Steps, res)
			if !res.Applied {
				b.FailedStep, b.FailedTool, b.Refused = i+1, "check", startLines(res.Refused, "", lines)
				return b, rollback()
			}
			kept, keptStep, keptNames = applied, i+1, len(names)
			continue
		}
		name := s.Refactoring.Name()
		_, isEdit := s.Refactoring.(Edit)
		_, isFix := s.Refactoring.(Fix)
		if opts.Unchecked && !isEdit && !isFix && len(ws.Errors()) > 0 {
			// A refactoring needs code that type-checks: supply now what
			// would otherwise be supplied at the end.
			if _, err := complete(i); err != nil {
				return nil, err
			}
		}
		// Names first, so that a line given as line_no is translated too.
		args, took := aliasArgs(s.Refactoring.InputSchema(), s.Args)
		if file, byLine := lineAddressed(args); byLine {
			rel := ws.Rel(ws.Abs(file))
			resolvedArgs, resolved, err := resolveRefs(args, rel, i, steps, made)
			if m := lines[rel]; err == nil && m != nil {
				resolvedArgs, err = translateLines(resolvedArgs, rel, m, resolved)
			}
			if err != nil {
				b.FailedStep, b.FailedTool, b.Refused = i+1, name, err.Error()
				return b, rollback()
			}
			args = resolvedArgs
		}
		res, err := Run(ws, s.Refactoring, args, Options{Compact: opts.Compact, Unchecked: opts.Unchecked})
		if err != nil {
			if rerr := rollback(); rerr != nil {
				return nil, fmt.Errorf("step %d (%s): %v; %v", i+1, name, err, rerr)
			}
			return nil, fmt.Errorf("step %d (%s): %w", i+1, name, err)
		}
		if took != "" {
			res.Notes = append(res.Notes, took)
		}
		b.Steps = append(b.Steps, res)
		if !res.Applied {
			b.FailedStep, b.FailedTool, b.Refused = i+1, name, startLines(res.Refused, stepFile(ws, args), lines)
			return b, rollback()
		}
		applied++
		names = append(names, name)
		followStep(ws, made, i, lines, res)
	}
	if opts.Unchecked {
		skipped, err := complete(len(steps))
		if err != nil {
			return nil, err
		}
		errs := ws.Errors()
		if len(errs) > 0 {
			b.Refused = startLines(fmt.Sprintf("after all %d step%s the code does not type-check, so the batch was rolled back. Errors were:\n  %s",
				len(steps), textutil.Plural(len(steps)), strings.Join(limit(errs, 10), "\n  ")), "", lines)
			for _, s := range skipped {
				b.Refused += "\n" + s
			}
			b.Errors, b.Notes = errs, skipped
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

// followStep records where step i (0-based), which made res, changed each
// file: it moves the batch's starting lines and the earlier steps' lines
// past it, and keeps the lines it made.
func followStep(ws *workspace.Workspace, made []map[string]*stepLines, i int, lines map[string]*lineMap, res *Result) {
	if c, ok := ws.LastChange(); ok {
		made[i] = map[string]*stepLines{}
		for p, before := range c.Before {
			rel := ws.Rel(p)
			var edits []edit.TextEdit
			for _, fe := range res.edits {
				if fe.Path == p {
					edits = append(edits, fe.Edits...)
				}
			}
			// Follow the lines through the step's own edits: a line diff can
			// pair a line with a copy the step moved elsewhere.
			moved := edit.LineMapOf(before, c.After[p], edits)
			if lines[rel] == nil {
				lines[rel] = newLineMap(before)
			}
			lines[rel].follow(i+1, moved)
			lines[rel].pos = append(lines[rel].pos, posStep{i + 1, edit.PosMapOf(before, c.After[p], edits)})
			lines[rel].cur = c.After[p]
			for _, m := range made[:i] {
				if s := m[rel]; s != nil {
					s.follow(i+1, moved, c.After[p])
				}
			}
			made[i][rel] = madeLines(before, c.After[p], moved, edits, c.Created[p])
		}
	}
}

// lineMap follows a file's lines through the steps of a batch. For each
// line (1-based) of the file as it was when the batch began, now is the
// line it is on now, or 0 once a step rewrote or removed it; changed is
// that step.
type lineMap struct {
	now     []int
	changed []int
	created bool // the file did not exist when the batch began
	// orig is the file when the batch began, cur as it is now, and pos
	// follows a byte offset of orig through each step since.
	orig, cur []byte
	pos       []posStep
}

// posStep is how one step moved a file's byte offsets (see edit.PosMapOf).
type posStep struct {
	step int
	at   func(off int) int
}

func newLineMap(before []byte) *lineMap {
	now := edit.LineMap(before, before)
	return &lineMap{now: now, changed: make([]int, len(now)), created: before == nil, orig: before, cur: before}
}

// follow moves m's lines through step, which moved the file's lines as
// moved says (see edit.LineMap).
func (m *lineMap) follow(step int, moved []int) {
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

// position returns where line and column (1-based, as when the batch
// began) are now, or the step that rewrote what was there.
func (m *lineMap) position(line, column int) (int, int, int) {
	off := 0
	for n := 1; n < line; n++ {
		i := bytes.IndexByte(m.orig[off:], '\n')
		if i < 0 {
			return 0, 0, 0
		}
		off += i + 1
	}
	off += column - 1
	for _, p := range m.pos {
		if off = p.at(off); off < 0 {
			return 0, 0, p.step
		}
	}
	start := bytes.LastIndexByte(m.cur[:off], '\n') + 1
	return bytes.Count(m.cur[:off], []byte("\n")) + 1, off - start + 1, 0
}

// translateLines rewrites a step's line arguments (line, start_line,
// end_line), given as of the start of the batch, to where those lines are
// now, except the keys in now, which already are. A line an earlier step
// rewrote or removed cannot be addressed.
func translateLines(args json.RawMessage, rel string, m *lineMap, now map[string]bool) (json.RawMessage, error) {
	var a map[string]json.RawMessage
	if err := json.Unmarshal(args, &a); err != nil {
		return args, nil
	}
	// A line with a column is followed as a position, which stays
	// addressable while what is at it does, even on a line a step changed.
	var line, column int
	if json.Unmarshal(a["line"], &line) == nil && json.Unmarshal(a["column"], &column) == nil &&
		line > 0 && column > 0 && !now["line"] && !m.created && line < len(m.now) {
		l, c, step := m.position(line, column)
		if step != 0 {
			return nil, fmt.Errorf("line %d, column %d of %s (as it was when the batch began) was rewritten by step %d, so it is not there to address; address what step %d made by name, or by reference ($%d.start, ...), or send this step in a later call with positions from a new inspect", line, column, rel, step, step, step)
		}
		if l > 0 {
			a["line"], _ = json.Marshal(l)
			a["column"], _ = json.Marshal(c)
			now = map[string]bool{"line": true, "start_line": now["start_line"], "end_line": now["end_line"]}
		}
	}
	for _, key := range []string{"line", "start_line", "end_line"} {
		var n int
		if raw, ok := a[key]; !ok || now[key] || json.Unmarshal(raw, &n) != nil || n <= 0 {
			continue
		}
		switch {
		case m.created:
			return nil, fmt.Errorf("%s was created in this batch, so it has no line %d from before it; address what is in it by name, or by reference to the step that created it ($2.start, $prev.end+3, ...)", rel, n)
		case n >= len(m.now):
			return nil, fmt.Errorf("%s had %d lines when the batch began, so there is no line %d", rel, len(m.now)-1, n)
		case m.now[n] == 0:
			return nil, fmt.Errorf("line %d of %s (as it was when the batch began) was rewritten by step %d, so it is not there to address; address what step %d made by name, or by reference ($%d.start, $%d.end+1, ...), or send this step in a later call with line numbers from a new inspect", n, rel, m.changed[n], m.changed[n], m.changed[n], m.changed[n])
		}
		a[key], _ = json.Marshal(m.now[n])
	}
	return json.Marshal(a)
}

// String renders the batch result as plain text, the format sent to models.
func (b *BatchResult) String() string {
	var sb strings.Builder
	if !b.Applied && b.Kept > 0 {
		if b.FailedStep > 0 {
			fmt.Fprintf(&sb, "REFUSED batch at step %d of %d (%s): %s\n", b.FailedStep, b.Total, b.FailedTool, b.Refused)
		} else {
			fmt.Fprintf(&sb, "REFUSED batch: %s\n", b.Refused)
		}
		fmt.Fprintf(&sb, "Steps 1-%d, through the checkpoint at step %d, were kept, as one change; the steps after it were rolled back.\n", b.Kept, b.Kept)
		if b.Diff != "" {
			sb.WriteString("\n" + b.Diff)
		}
		return sb.String()
	}
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
	for _, n := range b.Notes {
		fmt.Fprintf(&sb, "note: %s\n", n)
	}
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
		File      string          `json:"file"`
		Line      json.RawMessage `json:"line"` // a number, or a reference to an earlier step's lines
		StartLine json.RawMessage `json:"start_line"`
		EndLine   json.RawMessage `json:"end_line"`
		Column    int             `json:"column"`
	}
	if json.Unmarshal(args, &a) != nil || a.File == "" {
		return "", false
	}
	given := func(raw json.RawMessage) bool { return len(raw) > 0 && string(raw) != "0" && string(raw) != "null" }
	return a.File, given(a.Line) || given(a.StartLine) || given(a.EndLine) || a.Column != 0
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
