// Package tokens measures where a Claude Code session's tokens go, by tool,
// so that working through the refactoring tools can be compared with
// working by hand (Read, Edit, ...).
//
// A transcript (~/.claude/projects/<project>/<session>.jsonl) records the
// exact usage of every API request, but not of each tool call. The context
// a request sends grows by the previous request's output plus what came
// back in between, mostly tool results; that growth is split among the
// results by their size. A tool call's arguments are sized with the
// session's own characters-per-token ratio, measured on those results.
//
// The cost that matters most is not the first read of a result but every
// later request that sends it again: ContextTokens counts a call's tokens
// once per request that carried them, until the context shrank (/clear or
// compaction).
package tokens

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// ToolStats is the token cost of one tool's calls.
type ToolStats struct {
	Name          string
	Calls         int
	InputTokens   int // the calls' arguments (estimated)
	ResultTokens  int // what the calls returned
	ContextTokens int // input and result tokens, once per later request that sent them
}

// AlgebraStats is how much of the code that applied gorefactor calls
// changed the model wrote itself, and how much the tools derived, for one
// kind of call (Algebra...).
type AlgebraStats struct {
	Kind       string
	Calls      int // applied calls; a batch is one
	Written    int // lines of code the model supplied: content, new, field, value, type
	Changed    int // lines the results report as changed (calls that report them)
	Derived    int // changed lines beyond those written
	Unmeasured int // applied calls whose result reports no changed lines (a batch with report "summary")
}

// The kinds of call in Report.Algebra: only refactorings, only edits, or a
// batch of both.
const (
	AlgebraRefactoring = "refactorings"
	AlgebraEdit        = "edits"
	AlgebraMixed       = "batches of both"
)

// Report is the token cost of one or more sessions.
type Report struct {
	Requests         int
	BaseContext      int // context of the first request: system prompt, tools, CLAUDE.md
	InputTokens      int // summed over requests, cached or not
	OutputTokens     int
	CostUSD          float64        // from claude -p's result, when the input has one
	Turns            int            // likewise
	Finished         bool           // whether the input ends with claude -p's result; a session cut off by a timeout does not
	ToolTurns        int            // requests that called tools
	SchemaOnlyTurns  int            // of those, the ones that only loaded schemas (ToolSearch)
	Refused          map[string]int // turns with a refused call, by the kind of the first (Refused...)
	Reads            []ToolStats    // calls that read, by kind (inspect: file, Read, Grep, ...), most ContextTokens first
	Algebra          []AlgebraStats // applied gorefactor calls: what the model wrote and what the tools derived, by kind
	RefactoringSteps int            // applied steps that were refactorings (a batch's steps counted one by one)
	EditSteps        int            // and that were edits
	Tools            []ToolStats    // most ContextTokens first
}

// The kinds of refused call in Report.Refused.
const (
	RefusedWrite     = "Write or Edit, disabled"
	RefusedBashWrite = "Bash that writes, refused"
	RefusedBashRead  = "Bash that reads, refused"
	RefusedTool      = "gorefactor refused"
)

type line struct {
	Type            string  `json:"type"`
	IsSidechain     bool    `json:"isSidechain"`
	RequestID       string  `json:"requestId"`
	StreamRequestID string  `json:"request_id"`         // claude -p's spelling
	ParentToolUseID *string `json:"parent_tool_use_id"` // set on a subagent's turns in claude -p
	TotalCostUSD    float64 `json:"total_cost_usd"`     // on claude -p's result
	NumTurns        int     `json:"num_turns"`          // likewise
	Message         struct {
		Usage *struct {
			Input         int `json:"input_tokens"`
			CacheCreation int `json:"cache_creation_input_tokens"`
			CacheRead     int `json:"cache_read_input_tokens"`
			Output        int `json:"output_tokens"`
		} `json:"usage"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type block struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Text      string          `json:"text"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type call struct {
	name                    string
	inputChars, resultChars int
	resultTokens            float64
	after                   int // index of the first request that sent it
	input                   json.RawMessage
	refused                 string       // the kind of refusal it got (Refused...), or ""
	alg                     *algebraCall // for an applied gorefactor call, what it wrote and changed
	id                      string       // the tool_use id
}

type request struct {
	context, output int
	gapChars        int // characters that arrived after it: results and prompts
	calls           []*call
	results         []*call // results that arrived after it
}

// Analyze reads one transcript.
func Analyze(r io.Reader) (*Report, error) {
	return analyze(r, nil)
}

// analyze reads one transcript; with ids, only its requests from the first
// that called one of those tools to the last.
func analyze(r io.Reader, ids map[string]bool) (*Report, error) {
	var reqs []*request
	calls := map[string]*call{}
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	var cost float64 // from claude -p's result line
	var turns int
	finished := false
	for sc.Scan() {
		var l line
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.IsSidechain || l.ParentToolUseID != nil {
			continue
		}
		switch l.Type {
		case "result":
			cost, turns, finished = l.TotalCostUSD, l.NumTurns, true
		case "assistant":
			u := l.Message.Usage
			if u == nil {
				continue
			}
			id := l.RequestID
			if id == "" {
				id = l.StreamRequestID
			}
			if !seen[id] {
				seen[id] = true
				reqs = append(reqs, &request{context: u.Input + u.CacheCreation + u.CacheRead, output: u.Output})
			}
			cur := reqs[len(reqs)-1]
			for _, b := range blocks(l.Message.Content) {
				if b.Type == "tool_use" {
					c := &call{id: b.ID, name: b.Name, inputChars: len(b.Input), after: len(reqs), input: b.Input}
					calls[b.ID] = c
					cur.calls = append(cur.calls, c)
				}
			}
		case "user":
			if len(reqs) == 0 {
				continue
			}
			cur := reqs[len(reqs)-1]
			var text string
			if json.Unmarshal(l.Message.Content, &text) == nil {
				cur.gapChars += len(text)
				continue
			}
			for _, b := range blocks(l.Message.Content) {
				switch {
				case b.Type == "tool_result" && calls[b.ToolUseID] != nil:
					c := calls[b.ToolUseID]
					c.resultChars = textLen(b.Content)
					c.refused = refusal(c, b.IsError, resultText(b.Content))
					if strings.HasPrefix(c.name, "mcp__gorefactor__") {
						c.alg = measureAlgebra(c, resultText(b.Content))
					}
					cur.gapChars += c.resultChars
					cur.results = append(cur.results, c)
				case b.Type == "text":
					cur.gapChars += len(b.Text)
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if ids != nil {
		reqs = only(reqs, ids)
		cost, turns, finished = 0, 0, false
	}
	rep := report(reqs)
	rep.CostUSD, rep.Turns, rep.Finished = cost, turns, finished
	return rep, nil
}

// only is reqs from the first request that called a tool in ids to the
// last, each call's after rebased to the new indices; nil if none did.
func only(reqs []*request, ids map[string]bool) []*request {
	first, last := -1, -1
	for i, q := range reqs {
		for _, c := range q.calls {
			if ids[c.id] {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
	}
	if first < 0 {
		return nil
	}
	reqs = reqs[first : last+1]
	for _, q := range reqs {
		for _, c := range q.calls {
			c.after -= first
		}
	}
	return reqs
}

func blocks(raw json.RawMessage) []block {
	var bs []block
	json.Unmarshal(raw, &bs)
	return bs
}

// textLen is the length of a tool result's content: a string, or text blocks.
func textLen(raw json.RawMessage) int {
	return len(resultText(raw))
}

// resultText is a tool result's content: a string, or text blocks.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var sb strings.Builder
	for _, b := range blocks(raw) {
		sb.WriteString(b.Text)
	}
	return sb.String()
}

// writesFiles matches a shell command that changes files.
var writesFiles = regexp.MustCompile(`sed -i|perl -i|\b(mkdir|tee|cp|mv|rm|touch)\b|python3? -|git (add|commit|apply|checkout)|(^|[^0-9&>])>>?\s*([^&\s/]|/[^d])`)

// refusal returns the kind of refusal c got, given its result, or "": a
// Bash command that ran and failed ("Exit code 1") was not refused.
func refusal(c *call, isError bool, text string) string {
	switch {
	case strings.HasPrefix(c.name, "mcp__gorefactor__") && strings.HasPrefix(text, "REFUSED"):
		return RefusedTool
	case !isError:
		return ""
	case c.name == "Write" || c.name == "Edit" || c.name == "MultiEdit" || c.name == "NotebookEdit":
		return RefusedWrite
	case c.name != "Bash" || strings.HasPrefix(text, "Exit code"):
		return ""
	case writesFiles.MatchString(command(c.input)):
		return RefusedBashWrite
	}
	return RefusedBashRead
}

// command is a Bash call's command.
func command(input json.RawMessage) string {
	var a struct {
		Command string `json:"command"`
	}
	json.Unmarshal(input, &a)
	return a.Command
}

// checks matches a shell command that builds, tests or uses git rather
// than reads code.
var checks = regexp.MustCompile(`^\s*(cd \S+\s*(&&|;)\s*)?(go|gofmt|git) `)

// readKind is the kind of reading c did, for Report.Reads, or "" if it
// did not read.
func readKind(c *call) string {
	switch c.name {
	case "mcp__gorefactor__inspect":
		var a struct {
			File      string            `json:"file"`
			Function  string            `json:"function"`
			StartLine int               `json:"start_line"`
			Targets   []json.RawMessage `json:"targets"`
		}
		json.Unmarshal(c.input, &a)
		switch {
		case len(a.Targets) > 0:
			return "inspect: targets"
		case a.File == "":
			return "inspect: module overview"
		case a.Function != "" || a.StartLine != 0:
			return "inspect: part of a file"
		}
		return "inspect: file"
	case "Read", "Grep", "Glob":
		return c.name
	case "Bash":
		if c.refused == "" && !checks.MatchString(command(c.input)) {
			return "Bash, other than go and git"
		}
	}
	return ""
}

// minCalibrationChars is how many characters of results a session needs
// before its own characters-per-token ratio is trusted: each result also
// carries a few tokens of framing, which swamp a short one.
const minCalibrationChars = 100

func report(reqs []*request) *Report {
	r := &Report{Requests: len(reqs)}
	if len(reqs) == 0 {
		return r
	}
	r.BaseContext = reqs[0].context
	var tokens, chars float64
	for i, q := range reqs {
		r.InputTokens += q.context
		r.OutputTokens += q.output
		r.countTurn(q)
		if i+1 == len(reqs) || q.gapChars == 0 {
			continue
		}
		grew := reqs[i+1].context - q.context - q.output
		if grew <= 0 {
			continue
		}
		for _, c := range q.results {
			c.resultTokens = float64(grew) * float64(c.resultChars) / float64(q.gapChars)
			tokens += c.resultTokens
			chars += float64(c.resultChars)
		}
	}
	perChar := 1 / 3.5 // a common default, until the session gives its own
	if chars >= minCalibrationChars {
		perChar = tokens / chars
	}

	byName, byRead := map[string]*ToolStats{}, map[string]*ToolStats{}
	add := func(m map[string]*ToolStats, name string, in, res, carried int) {
		s := m[name]
		if s == nil {
			s = &ToolStats{Name: name}
			m[name] = s
		}
		s.Calls++
		s.InputTokens += in
		s.ResultTokens += res
		s.ContextTokens += (in + res) * carried
	}
	for _, q := range reqs {
		for _, c := range q.calls {
			if c.resultTokens == 0 {
				c.resultTokens = float64(c.resultChars) * perChar
			}
			in, res, carried := round(float64(c.inputChars)*perChar), round(c.resultTokens), carriedBy(reqs, c.after)
			add(byName, c.name, in, res, carried)
			if k := readKind(c); k != "" {
				add(byRead, k, in, res, carried)
			}
		}
	}
	for _, s := range byName {
		r.Tools = append(r.Tools, *s)
	}
	for _, s := range byRead {
		r.Reads = append(r.Reads, *s)
	}
	for _, q := range reqs {
		for _, c := range q.calls {
			if c.alg != nil {
				r.addAlgebra(c.alg)
			}
		}
	}
	r.sortTools()
	return r
}

// countTurn counts q in r's turns: whether it called tools, only loaded
// schemas, or had a call refused.
func (r *Report) countTurn(q *request) {
	if len(q.calls) == 0 {
		return
	}
	r.ToolTurns++
	only, refused := true, ""
	for _, c := range q.calls {
		only = only && c.name == "ToolSearch"
		if refused == "" {
			refused = c.refused
		}
	}
	if only {
		r.SchemaOnlyTurns++
	}
	if refused != "" {
		if r.Refused == nil {
			r.Refused = map[string]int{}
		}
		r.Refused[refused]++
	}
}

// algebraCall is what one applied gorefactor call changed: how many lines
// the model wrote, how many changed, and the kinds of its steps.
type algebraCall struct {
	kind                string
	written, changed    int
	measured            bool // the result reported its changed lines
	refactorings, edits int  // steps of each kind
}

// refactoringTools are the operations that restructure code, as opposed
// to the edits through which the model writes it.
var refactoringTools = map[string]bool{
	"rename": true, "extract_function": true, "extract_variable": true, "extract_struct": true,
	"inline_function": true, "inline_variable": true, "move_declaration": true, "move_parameter": true,
	"move_statement_up": true, "move_statement_down": true, "add_parameter": true, "remove_parameter": true,
	"introduce_parameter": true, "change_parameter_type": true, "add_result": true, "change_field_type": true,
	"change_type": true, "merge_duplicate_function": true, "function_to_method": true,
	"method_to_function": true, "remove_dead_code": true, "invert_if": true, "remove_else": true,
}

var (
	stepLine = regexp.MustCompile(`(?m)^\d+\. (\w+):`)
	hunk     = regexp.MustCompile(`(?m)^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@`)
	location = regexp.MustCompile(`^\s+\S+: ([\d, -]+)$`)
)

// measureAlgebra measures the call c of a gorefactor tool, whose result
// is text; nil for a call that changed nothing.
func measureAlgebra(c *call, text string) *algebraCall {
	tool := strings.TrimPrefix(c.name, "mcp__gorefactor__")
	if !strings.HasPrefix(text, "APPLIED ") {
		return nil
	}
	// The steps the call made, and of each the Go file it names (\"\" when
	// unknown, as for stubs a batch adds).
	type step struct {
		Tool string          `json:"tool"`
		Args json.RawMessage `json:"args"`
	}
	var steps []step
	if tool == "batch" {
		var in struct {
			Steps []step `json:"steps"`
		}
		json.Unmarshal(c.input, &in)
		for i, m := range stepLine.FindAllStringSubmatch(text, -1) {
			s := step{Tool: m[1]}
			if i < len(in.Steps) {
				s.Args = in.Steps[i].Args
			}
			steps = append(steps, s)
		}
	} else {
		steps = []step{{Tool: tool, Args: c.input}}
		if strings.HasPrefix(text, "APPLIED batch:") { // an edit run again with stubs
			for _, m := range stepLine.FindAllStringSubmatch(text, -1)[1:] {
				steps = append(steps, step{Tool: m[1]})
			}
		}
	}
	a := &algebraCall{}
	for _, s := range steps {
		if f := argFile(s.Args); f != "" && !strings.HasSuffix(f, ".go") {
			continue // text that is not code, such as a README
		}
		a.written += writtenLines(s.Args)
		switch {
		case refactoringTools[s.Tool]:
			a.refactorings++
		case s.Tool != "check":
			a.edits++
		}
	}
	if a.refactorings == 0 && a.edits == 0 {
		return nil
	}
	switch {
	case a.refactorings > 0 && a.edits > 0:
		a.kind = AlgebraMixed
	case a.refactorings > 0:
		a.kind = AlgebraRefactoring
	default:
		a.kind = AlgebraEdit
	}
	a.changed, a.measured = changedLines(text)
	return a
}

// writtenLines counts the lines of code in a call's arguments: what the
// model wrote for the tool to put in.
func writtenLines(args json.RawMessage) int {
	var m map[string]json.RawMessage
	json.Unmarshal(args, &m)
	n := 0
	for _, k := range []string{"content", "new", "field", "value", "type"} {
		var s string
		if json.Unmarshal(m[k], &s) == nil && strings.TrimSpace(s) != "" {
			n += strings.Count(strings.TrimRight(s, "\n"), "\n") + 1
		}
	}
	return n
}

// argFile is the file a call's arguments name: file, or the first of
// files; "" if none.
func argFile(args json.RawMessage) string {
	var a struct {
		File  string   `json:"file"`
		Files []string `json:"files"`
	}
	json.Unmarshal(args, &a)
	if a.File == "" && len(a.Files) > 0 {
		return a.Files[0]
	}
	return a.File
}

// changedLines counts the lines a result reports as changed: for each
// hunk of its diff, the larger of the lines it removed and added, or the
// lines a list of locations names. ok is false if it reports neither.
func changedLines(text string) (n int, ok bool) {
	count := func(s string) int {
		if s == "" {
			return 1
		}
		v, _ := strconv.Atoi(s)
		return v
	}
	goFile, inList := true, false
	for _, l := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(l, "+++ b/"):
			goFile = strings.HasSuffix(l, ".go")
			continue
		case l == "changed lines:":
			inList = true
			continue
		}
		if m := hunk.FindStringSubmatch(l); m != nil {
			if goFile {
				n += max(count(m[1]), count(m[2]))
			}
			ok = true
			continue
		}
		m := location.FindStringSubmatch(l)
		if !inList || m == nil {
			inList = inList && m != nil
			continue
		}
		ok = true
		if !strings.HasSuffix(strings.TrimSpace(strings.SplitN(l, ":", 2)[0]), ".go") {
			continue
		}
		for _, part := range strings.Split(m[1], ",") {
			from, to, isRange := strings.Cut(strings.TrimSpace(part), "-")
			a, _ := strconv.Atoi(from)
			b := a
			if isRange {
				b, _ = strconv.Atoi(to)
			}
			n += b - a + 1
		}
	}
	return n, ok
}

// addAlgebra adds a's stats to r.
func (r *Report) addAlgebra(a *algebraCall) {
	r.RefactoringSteps += a.refactorings
	r.EditSteps += a.edits
	s := AlgebraStats{Kind: a.kind, Calls: 1, Written: a.written}
	if a.measured {
		s.Changed, s.Derived = a.changed, max(a.changed-a.written, 0)
	} else {
		s.Unmeasured = 1
	}
	r.Algebra = mergeAlgebra(r.Algebra, []AlgebraStats{s})
}

// mergeAlgebra adds src's stats to dst's, by kind, keeping the kinds in
// the order refactorings, edits, batches of both.
func mergeAlgebra(dst, src []AlgebraStats) []AlgebraStats {
	for _, s := range src {
		i := slices.IndexFunc(dst, func(d AlgebraStats) bool { return d.Kind == s.Kind })
		if i < 0 {
			dst = append(dst, AlgebraStats{Kind: s.Kind})
			i = len(dst) - 1
		}
		d := &dst[i]
		d.Calls += s.Calls
		d.Written += s.Written
		d.Changed += s.Changed
		d.Derived += s.Derived
		d.Unmeasured += s.Unmeasured
	}
	order := []string{AlgebraRefactoring, AlgebraEdit, AlgebraMixed}
	slices.SortFunc(dst, func(a, b AlgebraStats) int { return slices.Index(order, a.Kind) - slices.Index(order, b.Kind) })
	return dst
}

// carriedBy counts the requests from index first on that sent what came
// before them: up to the first one whose context shrank.
func carriedBy(reqs []*request, first int) int {
	n := 0
	for i := first; i < len(reqs); i++ {
		if reqs[i].context < reqs[i-1].context {
			break
		}
		n++
	}
	return n
}

func round(f float64) int { return int(f + 0.5) }

func (r *Report) sortTools() {
	for _, s := range [][]ToolStats{r.Tools, r.Reads} {
		sort.Slice(s, func(i, j int) bool {
			if s[i].ContextTokens != s[j].ContextTokens {
				return s[i].ContextTokens > s[j].ContextTokens
			}
			return s[i].Name < s[j].Name
		})
	}
}

// Merge adds o's sessions to r.
func (r *Report) Merge(o *Report) {
	r.Requests += o.Requests
	r.BaseContext += o.BaseContext
	r.InputTokens += o.InputTokens
	r.OutputTokens += o.OutputTokens
	r.CostUSD += o.CostUSD
	r.Turns += o.Turns
	r.ToolTurns += o.ToolTurns
	r.SchemaOnlyTurns += o.SchemaOnlyTurns
	for k, n := range o.Refused {
		if r.Refused == nil {
			r.Refused = map[string]int{}
		}
		r.Refused[k] += n
	}
	r.Tools = mergeStats(r.Tools, o.Tools)
	r.Reads = mergeStats(r.Reads, o.Reads)
	r.Algebra = mergeAlgebra(r.Algebra, o.Algebra)
	r.RefactoringSteps += o.RefactoringSteps
	r.EditSteps += o.EditSteps
	r.sortTools()
}

// mergeStats adds src's stats to dst's, by name.
func mergeStats(dst, src []ToolStats) []ToolStats {
	byName := map[string]int{}
	for i, s := range dst {
		byName[s.Name] = i
	}
	for _, s := range src {
		i, ok := byName[s.Name]
		if !ok {
			byName[s.Name] = len(dst)
			dst = append(dst, ToolStats{Name: s.Name})
			i = len(dst) - 1
		}
		t := &dst[i]
		t.Calls += s.Calls
		t.InputTokens += s.InputTokens
		t.ResultTokens += s.ResultTokens
		t.ContextTokens += s.ContextTokens
	}
	return dst
}

const byHand = "by hand (Read, Edit, Write, Bash, Grep, Glob)"

func group(name string) string {
	switch {
	case strings.HasPrefix(name, "mcp__gorefactor__"):
		return "gorefactor"
	case name == "Read" || name == "Edit" || name == "Write" || name == "MultiEdit" ||
		name == "Bash" || name == "Grep" || name == "Glob":
		return byHand
	}
	return "other"
}

// Markdown renders the report: totals, then by group, then by tool.
func (r *Report) Markdown() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "- requests: %d\n- input tokens (cached or not): %d\n- output tokens: %d\n- first request's context: %d\n\n",
		r.Requests, r.InputTokens, r.OutputTokens, r.BaseContext)
	const head = "| %s | calls | input | result | context |\n|---|---|---|---|---|\n"
	row := func(s ToolStats) {
		fmt.Fprintf(&sb, "| %s | %d | %d | %d | %d |\n", s.Name, s.Calls, s.InputTokens, s.ResultTokens, s.ContextTokens)
	}
	groups := map[string]*ToolStats{}
	for _, s := range r.Tools {
		g := groups[group(s.Name)]
		if g == nil {
			g = &ToolStats{Name: group(s.Name)}
			groups[g.Name] = g
		}
		g.Calls += s.Calls
		g.InputTokens += s.InputTokens
		g.ResultTokens += s.ResultTokens
		g.ContextTokens += s.ContextTokens
	}
	fmt.Fprintf(&sb, head, "group")
	for _, name := range []string{"gorefactor", byHand, "other"} {
		if g := groups[name]; g != nil {
			row(*g)
		}
	}
	sb.WriteString("\n")
	fmt.Fprintf(&sb, head, "tool")
	for _, s := range r.Tools {
		row(s)
	}
	fmt.Fprintf(&sb, "\n| turns | count |\n|---|---|\n| turns with tool calls | %d |\n| ToolSearch only | %d |\n", r.ToolTurns, r.SchemaOnlyTurns)
	for _, k := range []string{RefusedWrite, RefusedBashWrite, RefusedBashRead, RefusedTool} {
		if n := r.Refused[k]; n > 0 {
			fmt.Fprintf(&sb, "| %s | %d |\n", k, n)
		}
	}
	if len(r.Reads) > 0 {
		sb.WriteString("\n")
		fmt.Fprintf(&sb, head, "read")
		for _, s := range r.Reads {
			row(s)
		}
	}
	if len(r.Algebra) > 0 {
		fmt.Fprintf(&sb, "\nsteps: %d refactorings, %d edits\n\n| algebra | calls | written | changed | derived | derived share | unmeasured |\n|---|---|---|---|---|---|---|\n", r.RefactoringSteps, r.EditSteps)
		all := AlgebraStats{Kind: "all"}
		arow := func(s AlgebraStats) {
			share := "-"
			if s.Changed > 0 {
				share = fmt.Sprintf("%d%%", round(100*float64(s.Derived)/float64(s.Changed)))
			}
			fmt.Fprintf(&sb, "| %s | %d | %d | %d | %d | %s | %d |\n", s.Kind, s.Calls, s.Written, s.Changed, s.Derived, share, s.Unmeasured)
		}
		for _, s := range r.Algebra {
			arow(s)
			all = mergeAlgebra([]AlgebraStats{all}, []AlgebraStats{{Kind: "all", Calls: s.Calls, Written: s.Written, Changed: s.Changed, Derived: s.Derived, Unmeasured: s.Unmeasured}})[0]
		}
		arow(all)
	}
	return sb.String()
}

// AnalyzeOnly reads one transcript, counting only its requests from the
// first that called a tool in ids to the last: one piece of work in a
// longer session, such as the calls a session log recorded on a branch.
func AnalyzeOnly(r io.Reader, ids map[string]bool) (*Report, error) {
	return analyze(r, ids)
}
