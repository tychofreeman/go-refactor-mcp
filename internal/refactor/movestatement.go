package refactor

import "encoding/json"

// MoveStatementOf is the "move_statement" tool of a workspace W: a
// language's move_statement_up and move_statement_down, published as one
// that takes a direction.
type MoveStatementOf[W Workspace] struct {
	Named[moveStatementArgs]
	up, down RefactoringOf[W]
	text     string
}

// moveStatementArgs are the arguments of move_statement.
type moveStatementArgs struct {
	File      string `json:"file" desc:"Path of the file, relative to the root."`
	Line      int    `json:"line" desc:"1-based line on which the statement to move starts."`
	Direction string `json:"direction" desc:"up, to swap it with the statement before it, or down, with the one after it."`
	PastTry   bool   `json:"past_try,omitempty" desc:"TypeScript: swap a statement with a try next to it whatever their side effects; the result says what changes (below the try, it runs only when the try completes normally)."`
}

// NewMoveStatement returns move_statement, which runs up or down as its
// direction says, described by description.
func NewMoveStatement[W Workspace](up, down RefactoringOf[W], description string) *MoveStatementOf[W] {
	return &MoveStatementOf[W]{Named: Called[moveStatementArgs]("move_statement"), up: up, down: down, text: description}
}

// Covers names the tools move_statement stands for.
func (m *MoveStatementOf[W]) Covers() []string { return []string{m.up.Name(), m.down.Name()} }

// Description is move_statement's, for the language it moves in.
func (m *MoveStatementOf[W]) Description() string { return m.text }

// Plan runs up or down, as direction says, with the arguments it takes.
func (m *MoveStatementOf[W]) Plan(ws W, raw json.RawMessage) (*Plan, error) {
	var a moveStatementArgs
	if err := DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	to := m.up
	switch a.Direction {
	case "up":
	case "down":
		to = m.down
	default:
		return nil, Refusef("direction is up or down, not %q", a.Direction)
	}
	return to.Plan(ws, OnlyArgs(raw, to.InputSchema()))
}
