package refactor

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

var (
	fileLine = regexp.MustCompile(`([\w./-]+\.go):(\d+)`)
	bareLine = regexp.MustCompile(`\b(lines? )(\d+)(-(\d+))?`)
	// A statement listed with its lines: 150-152 `if r > 0 {...`.
	listLine = regexp.MustCompile("\\b()(\\d+)(-(\\d+))? `")
)

// startLines rewrites the line numbers in msg, a refusal at a step of a
// batch, from the files as they are at that step to the files as they
// were when the batch began: the lines the model read, and gives again
// when it sends the batch anew. It rewrites "a.go:12" in any file, and
// "line 12" and "lines 12-14" in file, the step's (none if ""). A line
// an earlier step wrote was not there: it keeps its number, marked with
// *, and a note says so.
func startLines(msg, file string, lines map[string]*lineMap) string {
	starred := false
	back := func(rel, num string) string {
		m := lines[rel]
		n, err := strconv.Atoi(num)
		if m == nil || err != nil {
			return num
		}
		if !m.created {
			for orig, now := range m.now {
				if orig > 0 && now == n {
					return strconv.Itoa(orig)
				}
			}
		}
		starred = true
		return num + "*"
	}
	msg = fileLine.ReplaceAllStringFunc(msg, func(s string) string {
		g := fileLine.FindStringSubmatch(s)
		return g[1] + ":" + back(g[1], g[2])
	})
	for _, re := range []*regexp.Regexp{bareLine, listLine} {
		if file == "" {
			break
		}
		msg = re.ReplaceAllStringFunc(msg, func(s string) string {
			g := re.FindStringSubmatch(s)
			out := g[1] + back(file, g[2])
			if g[4] != "" {
				out += "-" + back(file, g[4])
			}
			return out + strings.TrimPrefix(g[0], g[1]+g[2]+g[3]) // what follows the lines in the match
		})
	}
	if starred {
		msg += "\n(* a line an earlier step of this batch wrote, numbered as it is after that step)"
	}
	return msg
}

// stepFile is the file a step's arguments name, relative to the root, or
// "" if they name none.
func stepFile(ws *workspace.Workspace, args json.RawMessage) string {
	var a struct {
		File string `json:"file"`
	}
	if json.Unmarshal(args, &a) != nil || a.File == "" {
		return ""
	}
	return ws.Rel(ws.Abs(a.File))
}
