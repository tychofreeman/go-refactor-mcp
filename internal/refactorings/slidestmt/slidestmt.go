// Package slidestmt implements move_statement_up and move_statement_down:
// refactorings that swap a statement with the one before or after it
// ("slide statements"), when the two are independent.
package slidestmt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// MoveStatement is the "move_statement_up" or, with down, the
// "move_statement_down" refactoring.
type MoveStatement struct{ down bool }

// NewUp returns the move_statement_up refactoring.
func NewUp() *MoveStatement { return &MoveStatement{} }

// NewDown returns the move_statement_down refactoring.
func NewDown() *MoveStatement { return &MoveStatement{down: true} }

func (m *MoveStatement) Name() string {
	if m.down {
		return "move_statement_down"
	}
	return "move_statement_up"
}

func (m *MoveStatement) Description() string {
	which, example := "before", `with log.Print("start") then n := len(xs), moving n := len(xs) up puts it first`
	if m.down {
		which, example = "after", `with log.Print("start") then n := len(xs), moving log.Print("start") down puts it second`
	}
	return "Swap a statement with the one " + which + " it in the same block, each with the comments directly above it: " + example + `. Use it to bring statements that belong together next to each other, e.g. before extract_function.
Identify the statement by file and the line it starts on (the outermost statement there, so a whole if or loop moves as one).
Behavior does not change, so it refuses, saying why, when the order matters: one uses what the other declares or assigns; one has side effects the other could see, or that would then happen or not when the other panics or does not finish; both could panic; a statement that returns or jumps out; a line with more than the statement on it.`
}

func (*MoveStatement) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file": {"type": "string",  "description": "Path of the file, relative to the module root."},
    "line": {"type": "integer", "description": "1-based line on which the statement to move starts."}
  },
  "required": ["file", "line"],
  "additionalProperties": false
}`)
}

type args struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

// Plan checks preconditions and computes the edits.
func (m *MoveStatement) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	pkg, file := ws.FileSyntax(path)
	if file == nil {
		return nil, refactor.Refusef("file %s is not part of the loaded packages", a.File)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	tf := ws.Fset.File(file.Pos())
	list, i := stmtAt(tf, file, a.Line)
	if i < 0 {
		return nil, refactor.Refusef("no statement starts on line %d", a.Line)
	}
	j, where := i-1, "above"
	if m.down {
		j, where = i+1, "below"
	}
	if j < 0 || j >= len(list) {
		end := map[bool]string{false: "first", true: "last"}[m.down]
		return nil, refactor.Refusef("the statement on line %d is the %s in its block; there is nothing to move it %s", a.Line, end, where)
	}
	first, second := list[min(i, j)], list[max(i, j)]
	aStart, aEnd, err := span(tf, file, src, first)
	if err != nil {
		return nil, err
	}
	bStart, bEnd, err := span(tf, file, src, second)
	if err != nil {
		return nil, err
	}
	f := analysis.AnalyzeFunc(pkg.TypesInfo, analysis.OutermostFunc(analysis.PathTo(file, first)))
	effects := ws.Effects()
	if err := checkIndependent(factsOf(f, tf, first, effects), factsOf(f, tf, second, effects)); err != nil {
		return nil, err
	}
	swapped := string(src[bStart:bEnd]) + string(src[aEnd:bStart]) + string(src[aStart:aEnd])
	return &refactor.Plan{
		Summary: fmt.Sprintf("moved the statement on line %d %s the one on line %d", a.Line, where, tf.Line(list[j].Pos())),
		Edits:   []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: aStart, End: bEnd, NewText: swapped}}}},
	}, nil
}

// stmtAt finds the outermost statement starting on line and the list it
// is an element of: a block's statements, or a case clause's.
func stmtAt(tf *token.File, file *ast.File, line int) (list []ast.Stmt, i int) {
	i = -1
	ast.Inspect(file, func(n ast.Node) bool {
		if i >= 0 || n == nil {
			return false
		}
		var l []ast.Stmt
		switch n := n.(type) {
		case *ast.BlockStmt:
			l = n.List
		case *ast.CaseClause:
			l = n.Body
		case *ast.CommClause:
			l = n.Body
		}
		for j, s := range l {
			switch s.(type) {
			case *ast.CaseClause, *ast.CommClause:
				continue
			}
			if tf.Line(s.Pos()) == line {
				list, i = l, j
				return false
			}
		}
		return true
	})
	return list, i
}

// span returns the byte range of the lines s is on, with the comments
// directly above it (no blank line between): from the start of the first
// to the start of the line after s. It refuses a statement that shares a
// line with other code; a comment at the end of its last line moves with
// it.
func span(tf *token.File, file *ast.File, src []byte, s ast.Stmt) (start, end int, err error) {
	line := tf.Line(s.Pos())
	start = tf.Offset(tf.LineStart(line))
	end = nextLine(src, tf.Offset(s.End()))
	rest := strings.TrimSpace(string(src[tf.Offset(s.End()):end]))
	if strings.TrimSpace(string(src[start:tf.Offset(s.Pos())])) != "" || (rest != "" && !strings.HasPrefix(rest, "//")) {
		return 0, 0, refactor.Refusef("line %d has code besides the statement; put each statement on its own line first", line)
	}
	for i := len(file.Comments) - 1; i >= 0; i-- {
		cg := file.Comments[i]
		if cg.End() >= s.Pos() {
			continue
		}
		first := tf.Offset(tf.LineStart(tf.Line(cg.Pos())))
		if tf.Line(cg.End()) != tf.Line(tf.Pos(start))-1 || strings.TrimSpace(string(src[first:tf.Offset(cg.Pos())])) != "" {
			break
		}
		start = first
	}
	return start, end, nil
}

// nextLine returns the offset of the start of the line after off.
func nextLine(src []byte, off int) int {
	if i := bytes.IndexByte(src[off:], '\n'); i >= 0 {
		return off + i + 1
	}
	return len(src)
}
