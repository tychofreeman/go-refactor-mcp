package refactortest

import (
	"strings"
	"testing"
)

func TestGoTestCommandLeavesOutStressVariables(t *testing.T) {
	t.Setenv("STRESS_DIR", "/tmp/copy")
	t.Setenv("STRESS_TESTS", "1")
	t.Setenv("KEEP_ME", "yes")
	cmd := GoTestCommand("/root", "a/b/c.go")
	if cmd.Dir != "/root" || strings.Join(cmd.Args, " ") != "go test -count=1 ./a/b" {
		t.Errorf("command = %v in %s", cmd.Args, cmd.Dir)
	}
	kept := false
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "STRESS_") {
			t.Errorf("environment has %s", kv)
		}
		kept = kept || kv == "KEEP_ME=yes"
	}
	if !kept {
		t.Error("environment lost KEEP_ME")
	}
}
