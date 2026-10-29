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

// CheckpointOf is the "check" step of a batch: the code must type-check
// there, and with run_tests its tests must pass. When a later step is
// refused, the batch keeps the steps up to its last checkpoint that
// passed and rolls back only those after it.
type CheckpointOf[W Workspace] struct{}

// Checkpoint is the check step of a batch on Go code.
type Checkpoint = CheckpointOf[*workspace.Workspace]

// NewCheckpoint returns the check step of a batch.
func NewCheckpoint() *Checkpoint { return &Checkpoint{} }

func (*CheckpointOf[W]) Name() string { return "check" }

func (*CheckpointOf[W]) Description() string {
	return `A checkpoint in a batch: the code must type-check here (after the stubs and imports earlier edits need), and with run_tests the tests of packages must pass. If a later step is refused, the steps up to the last checkpoint that passed are kept, and only those after it are rolled back.`
}

func (*CheckpointOf[W]) InputSchema() json.RawMessage {
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
func (*CheckpointOf[W]) Plan(W, json.RawMessage) (*Plan, error) {
	return nil, Refusef("check is a checkpoint, a step of a batch; on its own, use the check tool")
}

// checkpoint runs a check step: complete adds what earlier edits need,
// in a batch checked at its end; then the code must type-check, and the
// tests pass if asked for. Its result is refused if not. Where the
// workspace tolerates errors, the code must have none that the batch did
// not begin with (baseline).
func checkpoint[W Workspace](ws W, raw json.RawMessage, opts OptionsOf[W], baseline []string, complete func() error) (*Result, error) {
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
	errs := ws.Errors()
	if tolerates(ws) {
		errs = addedErrors(baseline, errs)
	}
	if len(errs) > 0 {
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
			test = func(p string) (string, error) { return GoTest(ws.Dir(), p) }
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
	return goTest(dir, pattern)
}

// GoTestRace is GoTest with the race detector on.
func GoTestRace(dir, pattern string) (string, error) {
	return goTest(dir, "-race", pattern)
}

// goTest runs go test with args in dir, within TestTimeout.
func goTest(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), TestTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", append([]string{"test"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("timed out after %s", TestTimeout)
	}
	return string(out), err
}

// CheckReport is the check tool's report on a module whose type errors
// are errs and, with run_tests in raw, on the tests of packages, run in
// root; passed says whether it type-checks and the tests pass. Arguments
// it does not take are refused.
func CheckReport(errs []string, root string, raw json.RawMessage) (report string, passed bool, err error) {
	var a struct {
		RunTests bool   `json:"run_tests"`
		Packages string `json:"packages"`
		Race     bool   `json:"race"`
	}
	if err := DecodeArgs(raw, &a); err != nil {
		return "", false, err
	}
	var sb strings.Builder
	passed = len(errs) == 0
	if passed {
		sb.WriteString("TYPE-CHECK OK: the module compiles.\n")
	} else {
		fmt.Fprintf(&sb, "TYPE-CHECK FAILED (%d errors):\n  %s\n", len(errs), strings.Join(errs, "\n  "))
	}
	if !a.RunTests {
		return sb.String(), passed, nil
	}
	pattern := a.Packages
	if pattern == "" {
		pattern = "./..."
	}
	if strings.HasPrefix(pattern, "-") {
		return "", false, Refusef("packages must be a package pattern, not a flag")
	}
	run := GoTest
	if a.Race {
		run = GoTestRace
	}
	out, testErr := run(root, pattern)
	if testErr != nil {
		fmt.Fprintf(&sb, "\nTESTS FAILED (%v):\n", testErr)
		passed = false
	} else {
		sb.WriteString("\nTESTS PASSED:\n")
	}
	sb.WriteString(lastLines(out, 150) + "\n")
	return sb.String(), passed, nil
}

// lastLines keeps the last n lines of s.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = append([]string{fmt.Sprintf("... (%d lines before)", len(lines)-n)}, lines[len(lines)-n:]...)
	}
	return strings.Join(lines, "\n")
}
