package tokens

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
)

// Event is one line of a session log (cmd/session-log): a hook's input
// for one tool call, before it runs (PreToolUse) or after (PostToolUse),
// with the text of the result in place of the response.
type Event struct {
	Time        string          `json:"time,omitempty"`
	Hook        string          `json:"hook_event_name"`
	Session     string          `json:"session_id,omitempty"`
	ToolUseID   string          `json:"tool_use_id"`
	Tool        string          `json:"tool_name"`
	Input       json.RawMessage `json:"tool_input"`
	Result      string          `json:"result,omitempty"`
	ResultChars int             `json:"result_chars,omitempty"`
}

// WorkReport is how a piece of work used the tools, from its session log:
// what went through gorefactor, what was done by hand, and what was done
// by hand after gorefactor refused. A log has no token counts; for those,
// Analyze the session's transcript.
type WorkReport struct {
	Calls int // tool calls
	// NoResult are the calls that got no PostToolUse: denied by a hook
	// (read-guard) or the user, or failed.
	NoResult         []string
	Reads            map[string]int // calls that read, by kind (as Report.Reads)
	Refused          map[string]int // gorefactor calls refused (RefusedTool)
	HandWrites       []string       // writes not through the tools: Edit, Write, and Bash that changed files
	AfterRefusal     []string       // gorefactor refusals whose next write was by hand
	Algebra          []AlgebraStats // applied gorefactor calls, as Report.Algebra
	RefactoringSteps int
	EditSteps        int
	CLI              int // calls of the refactor CLI through Bash, which are not measured
}

// cliCommand matches a Bash command that runs the refactor CLI.
var cliCommand = regexp.MustCompile(`(^|[\s/])refactor\s`)

// AnalyzeWork reads one session log.
func AnalyzeWork(r io.Reader) (*WorkReport, error) {
	type logged struct {
		Event
		ran bool
	}
	var order []*logged
	byID := map[string]*logged{}
	sc := bufio.NewScanner(r)
	sc.Buffer(nil, 64<<20)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.ToolUseID == "" {
			continue
		}
		c := byID[e.ToolUseID]
		if c == nil {
			c = &logged{Event: e}
			byID[e.ToolUseID] = c
			order = append(order, c)
		}
		if e.Hook == "PostToolUse" {
			c.Event, c.ran = e, true
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	w := &WorkReport{Calls: len(order), Reads: map[string]int{}, Refused: map[string]int{}}
	var alg Report
	var refused []string // refusals not yet followed by an applied call or a write by hand
	for _, c := range order {
		k := &call{name: c.Tool, input: c.Input}
		what := describe(c.Tool, c.Input)
		switch {
		case !c.ran:
			w.NoResult = append(w.NoResult, what)
		case writesByHand(k):
			w.HandWrites = append(w.HandWrites, what)
			for _, r := range refused {
				w.AfterRefusal = append(w.AfterRefusal, r+", then "+what)
			}
			refused = nil
		case strings.HasPrefix(c.Result, "REFUSED"):
			if why := refusal(k, false, c.Result); why != "" {
				w.Refused[why]++
			}
			name := strings.TrimPrefix(c.Tool, "mcp__gorefactor__")
			if c.Tool == "Bash" {
				w.CLI++
				name = "refactor CLI"
			}
			refused = append(refused, fmt.Sprintf("%s (%s)", name, firstLine(c.Result)))
		case c.Tool == "Bash" && cliCommand.MatchString(command(c.Input)):
			w.CLI++
			if strings.HasPrefix(c.Result, "APPLIED ") {
				refused = nil
			}
		default:
			if kind := readKind(k); kind != "" {
				w.Reads[kind]++
			}
			if a := measureAlgebra(k, c.Result); a != nil {
				alg.addAlgebra(a)
			}
			if strings.HasPrefix(c.Result, "APPLIED ") {
				refused = nil
			}
		}
	}
	w.Algebra, w.RefactoringSteps, w.EditSteps = alg.Algebra, alg.RefactoringSteps, alg.EditSteps
	return w, nil
}

// writesByHand reports whether c changed files other than through the
// tools: Edit or Write, or a Bash command that writes and is not go, git
// or the refactor CLI.
func writesByHand(c *call) bool {
	switch c.name {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return true
	case "Bash":
		cmd := command(c.input)
		return !cliCommand.MatchString(cmd) && !checks.MatchString(cmd) && writesFiles.MatchString(cmd)
	}
	return false
}

// describe names a call for the report: the tool and the file or command.
func describe(tool string, input json.RawMessage) string {
	var a struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Command      string `json:"command"`
	}
	json.Unmarshal(input, &a)
	switch {
	case tool == "Bash":
		cmd := firstLine(a.Command)
		if len(cmd) > 120 {
			cmd = cmd[:120] + "..."
		}
		return "Bash: " + cmd
	case a.FilePath != "":
		return tool + " " + a.FilePath
	case a.NotebookPath != "":
		return tool + " " + a.NotebookPath
	case strings.HasPrefix(tool, "mcp__gorefactor__"):
		name := strings.TrimPrefix(tool, "mcp__gorefactor__")
		if f := argFile(input); f != "" {
			return name + " " + f
		}
		return name
	}
	return tool
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// Markdown is the report as Markdown, for a session record
// (docs/sessions/).
func (w *WorkReport) Markdown() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "- tool calls: %d\n", w.Calls)
	fmt.Fprintf(&sb, "- through gorefactor: %d refactoring steps, %d edit steps", w.RefactoringSteps, w.EditSteps)
	if w.CLI > 0 {
		fmt.Fprintf(&sb, ", and %d calls of the CLI (not counted in steps)", w.CLI)
	}
	sb.WriteString("\n")
	fmt.Fprintf(&sb, "- refused by gorefactor: %d\n", w.Refused[RefusedTool])
	list := func(title string, items []string) {
		fmt.Fprintf(&sb, "- %s: %d\n", title, len(items))
		for _, s := range items {
			fmt.Fprintf(&sb, "  - %s\n", s)
		}
	}
	list("by hand", w.HandWrites)
	list("refused, then done by hand", w.AfterRefusal)
	list("denied or failed (no result)", w.NoResult)
	if len(w.Reads) > 0 {
		kinds := make([]string, 0, len(w.Reads))
		for k := range w.Reads {
			kinds = append(kinds, k)
		}
		slices.Sort(kinds)
		var parts []string
		for _, k := range kinds {
			parts = append(parts, fmt.Sprintf("%s %d", k, w.Reads[k]))
		}
		fmt.Fprintf(&sb, "- reads: %s\n", strings.Join(parts, ", "))
	}
	if len(w.Algebra) > 0 {
		sb.WriteString("\n| kind | calls | written | changed | derived |\n|---|---|---|---|---|\n")
		var all AlgebraStats
		for _, s := range w.Algebra {
			fmt.Fprintf(&sb, "| %s | %d | %d | %d | %d |\n", s.Kind, s.Calls, s.Written, s.Changed, s.Derived)
			all.Calls, all.Written, all.Changed, all.Derived = all.Calls+s.Calls, all.Written+s.Written, all.Changed+s.Changed, all.Derived+s.Derived
		}
		fmt.Fprintf(&sb, "| all | %d | %d | %d | %d |\n", all.Calls, all.Written, all.Changed, all.Derived)
		if all.Changed > 0 {
			fmt.Fprintf(&sb, "\nDerived share: %d%% of the Go lines changed through gorefactor.\n", 100*all.Derived/all.Changed)
		}
	}
	return sb.String()
}
