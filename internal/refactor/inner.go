package refactor

import (
	"encoding/json"

	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// InnerOf is a step of another refactoring's Expand, never a tool of its
// own: Tool is the name of the refactoring it is a step of, Schema its
// arguments, and Do plans it.
type InnerOf[W Workspace] struct {
	Tool   string
	Schema json.RawMessage
	Do     func(ws W, args json.RawMessage) (*Plan, error)
}

func (s InnerOf[W]) Name() string                 { return s.Tool }
func (InnerOf[W]) Description() string            { return "" }
func (s InnerOf[W]) InputSchema() json.RawMessage { return s.Schema }

func (s InnerOf[W]) Plan(ws W, args json.RawMessage) (*Plan, error) { return s.Do(ws, args) }

// InnerEditOf is an InnerOf that changes behavior on purpose: a later step
// of the same Expand shows that it did not.
type InnerEditOf[W Workspace] struct{ InnerOf[W] }

func (InnerEditOf[W]) ChangesBehavior() {}

// Inner and InnerEdit are InnerOf and InnerEditOf for Go.
type (
	Inner     = InnerOf[*workspace.Workspace]
	InnerEdit = InnerEditOf[*workspace.Workspace]
)

// WriteThenRemove returns the steps of a change made in two for each of n
// things: write for every one, then remove for every one, each with the
// arguments args gives for it. The writes all come first, so that each
// removal is checked against every write.
func WriteThenRemove[W Workspace](n int, write, remove RefactoringOf[W], args func(i int) (json.RawMessage, error)) ([]StepOf[W], error) {
	steps := make([]StepOf[W], 2*n)
	for i := range n {
		a, err := args(i)
		if err != nil {
			return nil, err
		}
		steps[i] = StepOf[W]{Refactoring: write, Args: a}
		steps[n+i] = StepOf[W]{Refactoring: remove, Args: a}
	}
	return steps, nil
}
