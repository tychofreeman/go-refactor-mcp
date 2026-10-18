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
	"sort"
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

// Report is the token cost of one or more sessions.
type Report struct {
	Requests     int
	BaseContext  int // context of the first request: system prompt, tools, CLAUDE.md
	InputTokens  int // summed over requests, cached or not
	OutputTokens int
	CostUSD      float64     // from claude -p's result, when the input has one
	Turns        int         // likewise
	Tools        []ToolStats // most ContextTokens first
}

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
}

type call struct {
	name                    string
	inputChars, resultChars int
	resultTokens            float64
	after                   int // index of the first request that sent it
}

type request struct {
	context, output int
	gapChars        int // characters that arrived after it: results and prompts
	calls           []*call
	results         []*call // results that arrived after it
}

// Analyze reads one transcript.
func Analyze(r io.Reader) (*Report, error) {
	var reqs []*request
	calls := map[string]*call{}
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	var cost float64 // from claude -p's result line
	var turns int
	for sc.Scan() {
		var l line
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.IsSidechain || l.ParentToolUseID != nil {
			continue
		}
		switch l.Type {
		case "result":
			cost, turns = l.TotalCostUSD, l.NumTurns
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
					c := &call{name: b.Name, inputChars: len(b.Input), after: len(reqs)}
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
	rep := report(reqs)
	rep.CostUSD, rep.Turns = cost, turns
	return rep, nil
}

func blocks(raw json.RawMessage) []block {
	var bs []block
	json.Unmarshal(raw, &bs)
	return bs
}

// textLen is the length of a tool result's content: a string, or text blocks.
func textLen(raw json.RawMessage) int {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return len(s)
	}
	n := 0
	for _, b := range blocks(raw) {
		n += len(b.Text)
	}
	return n
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

	byName := map[string]*ToolStats{}
	for _, q := range reqs {
		for _, c := range q.calls {
			s := byName[c.name]
			if s == nil {
				s = &ToolStats{Name: c.name}
				byName[c.name] = s
			}
			if c.resultTokens == 0 {
				c.resultTokens = float64(c.resultChars) * perChar
			}
			in, res := round(float64(c.inputChars)*perChar), round(c.resultTokens)
			s.Calls++
			s.InputTokens += in
			s.ResultTokens += res
			s.ContextTokens += (in + res) * carriedBy(reqs, c.after)
		}
	}
	for _, s := range byName {
		r.Tools = append(r.Tools, *s)
	}
	r.sortTools()
	return r
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
	sort.Slice(r.Tools, func(i, j int) bool {
		if r.Tools[i].ContextTokens != r.Tools[j].ContextTokens {
			return r.Tools[i].ContextTokens > r.Tools[j].ContextTokens
		}
		return r.Tools[i].Name < r.Tools[j].Name
	})
}

// Merge adds o's sessions to r.
func (r *Report) Merge(o *Report) {
	r.Requests += o.Requests
	r.BaseContext += o.BaseContext
	r.InputTokens += o.InputTokens
	r.OutputTokens += o.OutputTokens
	r.CostUSD += o.CostUSD
	r.Turns += o.Turns
	byName := map[string]int{}
	for i, s := range r.Tools {
		byName[s.Name] = i
	}
	for _, s := range o.Tools {
		i, ok := byName[s.Name]
		if !ok {
			byName[s.Name] = len(r.Tools)
			r.Tools = append(r.Tools, ToolStats{Name: s.Name})
			i = len(r.Tools) - 1
		}
		t := &r.Tools[i]
		t.Calls += s.Calls
		t.InputTokens += s.InputTokens
		t.ResultTokens += s.ResultTokens
		t.ContextTokens += s.ContextTokens
	}
	r.sortTools()
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
	return sb.String()
}
