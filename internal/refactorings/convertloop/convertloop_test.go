package convertloop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
)

func TestGolden(t *testing.T) {
	refactortest.Run(t, New(), "testdata/convert_loop")
}

// TestProbes runs each probe_ case's program before and after: a
// conversion keeps what it prints.
func TestProbes(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go")
	}
	dirs, err := filepath.Glob("testdata/convert_loop/probe_*")
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no probes: %v", err)
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			before, after := run(t, filepath.Join(dir, "in")), run(t, filepath.Join(dir, "out"))
			if before != after {
				t.Errorf("the program printed\n%s\nbefore, and\n%s\nafter", before, after)
			}
		})
	}
}

func run(t *testing.T, dir string) string {
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run in %s: %v\n%s", dir, err, out)
	}
	return strings.TrimSpace(string(out))
}
