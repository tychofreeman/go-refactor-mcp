package simplify

// Opt-in stress test: tries inline on the local variables (STRESS_KIND
// variables, the default) or on every call of a file's own functions
// (functions) of a real TypeScript project, or a random sample, and reports
// each change the type check rolled back (a precondition is missing) or,
// with STRESS_TEST_CMD, after which the project's tests failed (a behavior
// change). {dir} in the command is the changed file's directory. It
// modifies STRESS_DIR, so point it at a copy:
//
//	STRESS_DIR=/tmp/hono [STRESS_KIND=functions] [STRESS_N=200] [STRESS_SEED=1] \
//	[STRESS_TEST_CMD='npx vitest run {dir}'] go test -run TestStress -v -timeout 2h .

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

var (
	localDecl = regexp.MustCompile(`^\s*(?:const|let)\s+([A-Za-z_$][\w$]*)\s*[:=]`)
	fileFunc  = regexp.MustCompile(`^function\s+([A-Za-z_$][\w$]*)\s*[(<]`)
)

func TestStress(t *testing.T) {
	dir := os.Getenv("STRESS_DIR")
	if dir == "" {
		t.Skip("set STRESS_DIR")
	}
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cands := stressCandidates(t, dir, os.Getenv("STRESS_KIND"))
	rand.New(rand.NewSource(int64(envInt("STRESS_SEED", 1)))).Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
	n := min(envInt("STRESS_N", 200), len(cands))
	applied, refused := 0, map[string]int{}
	var bugs []string
	for _, args := range cands[:n] {
		res, err := refactor.Run(ws, NewInline(), json.RawMessage(args), refactor.OptionsOf[*workspace.Workspace]{})
		switch {
		case err != nil:
			bugs = append(bugs, fmt.Sprintf("ERROR %s: %v", args, err))
		case res.Applied:
			applied++
			if cmd := os.Getenv("STRESS_TEST_CMD"); cmd != "" {
				var a struct{ File string }
				_ = json.Unmarshal([]byte(args), &a)
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(envInt("STRESS_TEST_TIMEOUT", 300))*time.Second)
				c := exec.CommandContext(ctx, "sh", "-c", strings.ReplaceAll(cmd, "{dir}", filepath.Dir(a.File)))
				c.Dir = dir
				// The test runner's workers outlive sh: kill the whole group on a timeout.
				c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
				c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
				c.WaitDelay = 5 * time.Second
				out, err := c.CombinedOutput()
				switch {
				case ctx.Err() != nil:
					bugs = append(bugs, fmt.Sprintf("TESTS HANG %s:\n%s", args, res.Diff))
				case err != nil:
					bugs = append(bugs, fmt.Sprintf("TESTS FAIL %s:\n%s\n%s", args, res.Diff, lastLines(string(out), 15)))
				}
				cancel()
			}
			if _, err := ws.Undo(); err != nil {
				t.Fatalf("undo: %v", err)
			}
		case strings.Contains(res.Refused, "adds errors") || strings.Contains(res.Refused, "rolled back"):
			bugs = append(bugs, fmt.Sprintf("ROLLBACK %s:\n%s", args, res.Refused))
		default:
			r, _, _ := strings.Cut(res.Refused, ":")
			refused[strings.Map(func(c rune) rune {
				if c >= '0' && c <= '9' {
					return -1
				}
				return c
			}, r)]++
		}
	}
	keys := make([]string, 0, len(refused))
	for k := range refused {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return refused[keys[i]] > refused[keys[j]] })
	t.Logf("tried %d of %d: applied %d, bugs %d", n, len(cands), applied, len(bugs))
	for _, k := range keys {
		t.Logf("  %4d refused: %s", refused[k], k)
	}
	for _, b := range bugs {
		t.Errorf("%s", b)
	}
}

// stressCandidates returns inline's arguments for each candidate of kind
// in the source files under dir/src, tests and declaration files left out.
func stressCandidates(t *testing.T, dir, kind string) []string {
	var out []string
	err := filepath.WalkDir(filepath.Join(dir, "src"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".d.ts") || strings.Contains(p, ".test.") {
			return err
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		for i, line := range strings.Split(string(src), "\n") {
			var args map[string]any
			switch kind {
			case "functions":
				if m := fileFunc.FindStringSubmatch(line); m != nil {
					args = map[string]any{"file": rel, "name": m[1], "all_calls": true}
				}
			default:
				if m := localDecl.FindStringSubmatch(line); m != nil && strings.HasPrefix(line, " ") {
					args = map[string]any{"file": rel, "line": i + 1, "name": m[1]}
				}
			}
			if args != nil {
				b, _ := json.Marshal(args)
				out = append(out, string(b))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func envInt(k string, def int) int {
	var n int
	if _, err := fmt.Sscan(os.Getenv(k), &n); err != nil {
		return def
	}
	return n
}

// lastLines is the last n lines of s.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}
