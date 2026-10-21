package refactor

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// Checkpoint is the "check" step of a batch: the code must type-check
// there, and with run_tests its tests must pass. When a later step is
// refused, the batch keeps the steps up to its last checkpoint that
// passed and rolls back only those after it.
type Checkpoint struct{}

// NewCheckpoint returns the check step of a batch.
func NewCheckpoint() *Checkpoint { return &Checkpoint{} }

func (*Checkpoint) Name() string { return "check" }

func (*Checkpoint) Description() string {
	return `A checkpoint in a batch: the code must type-check here (after the stubs and imports earlier edits need), and with run_tests the tests of packages must pass. If a later step is refused, the steps up to the last checkpoint that passed are kept, and only those after it are rolled back.`
}

func (*Checkpoint) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "run_tests": {"type": "boolean", "description": "Also run go test, which must pass."},
    "packages":  {"type": "string",  "description": "The package pattern to test (default ./...)."}
  },
  "additionalProperties": false
}`)
}

// Plan refuses: a checkpoint is a step of a batch, which runs it itself.
func (*Checkpoint) Plan(*workspace.Workspace, json.RawMessage) (*Plan, error) {
	return nil, Refusef("check is a checkpoint, a step of a batch; on its own, use the check tool")
}

// checkpoint runs a check step: complete adds what earlier edits need,
// in a batch checked at its end; then the code must type-check, and the
// tests pass if asked for. Its result is refused if not.
func checkpoint(ws *workspace.Workspace, raw json.RawMessage, opts Options, complete func() error) (*Result, error) {
	res := &Result{Refactoring: "check"}
	var a struct {
		RunTests bool   `json:"run_tests"`
		Packages string `json:"packages"`
	}
	if err := DecodeArgs(raw, &a); err != nil {
		res.Refused = err.Error()
		return res, nil
	}
	if opts.Unchecked && len(ws.Errors()) > 0 {
		if err := complete(); err != nil {
			return nil, err
		}
	}
	if errs := ws.Errors(); len(errs) > 0 {
		res.Refused = fmt.Sprintf("the code does not type-check at this checkpoint. Errors were:\n  %s", strings.Join(limit(errs, 10), "\n  "))
		return res, nil
	}
	res.Summary = "type-checks"
	if a.RunTests {
		pattern := a.Packages
		if pattern == "" {
			pattern = "./..."
		}
		if strings.HasPrefix(pattern, "-") {
			res.Refused = "packages must be a package pattern, not a flag"
			return res, nil
		}
		test := opts.Test
		if test == nil {
			test = func(p string) (string, error) { return GoTest(ws.Root, p) }
		}
		if out, err := test(pattern); err != nil {
			res.Refused = fmt.Sprintf("the tests of %s failed at this checkpoint (%v):\n%s", pattern, err, lastLines(out, 60))
			return res, nil
		}
		res.Summary += "; tests pass"
	}
	res.Applied = true
	return res, nil
}

// TestTimeout bounds GoTest, so that a hanging test cannot stall a batch.
var TestTimeout = 10 * time.Minute

// GoTest runs go test on pattern in dir and returns its output; the error
// says why the tests did not pass.
func GoTest(dir, pattern string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), TestTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", pattern)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("timed out after %s", TestTimeout)
	}
	return string(out), err
}

// lastLines keeps the last n lines of s.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = append([]string{fmt.Sprintf("... (%d lines before)", len(lines)-n)}, lines[len(lines)-n:]...)
	}
	return strings.Join(lines, "\n")
}
