package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// checkout makes a main checkout with a worktree inside it, as
// EnterWorktree and worktree isolation make them, and returns both.
func checkout(t *testing.T) (project, tree string) {
	t.Helper()
	project = t.TempDir()
	tree = filepath.Join(project, ".claude", "worktrees", "agent-1")
	for _, d := range []string{filepath.Join(project, ".git"), filepath.Join(tree, "internal", "plant")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(tree, ".git"), []byte("gitdir: "+project+"/.git/worktrees/agent-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return project, tree
}

func TestPlaceInWorktree(t *testing.T) {
	project, tree := checkout(t)
	in := map[string]any{"report": "summary", "steps": []any{
		map[string]any{"tool": "rename", "args": map[string]any{"file": "internal/plant/plant.go", "name": "Water", "new_name": "Irrigate"}},
		map[string]any{"tool": "replace_text", "args": map[string]any{"files": []any{"README.md", "docs/a.md"}, "old": "x", "new": "y"}},
	}}
	got, refused := place(in, filepath.Join(tree, "internal"), project)
	if refused != "" {
		t.Fatalf("refused: %s", refused)
	}
	want := map[string]any{"report": "summary", "steps": []any{
		map[string]any{"tool": "rename", "args": map[string]any{"file": filepath.Join(tree, "internal/plant/plant.go"), "name": "Water", "new_name": "Irrigate"}},
		map[string]any{"tool": "replace_text", "args": map[string]any{"files": []any{filepath.Join(tree, "README.md"), filepath.Join(tree, "docs/a.md")}, "old": "x", "new": "y"}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestPlaceInMainCheckout(t *testing.T) {
	project, _ := checkout(t)
	in := map[string]any{"file": "internal/plant/plant.go"}
	if got, refused := place(in, project, project); got != nil || refused != "" {
		t.Errorf("in the main checkout: got %v, refused %q; want the call unchanged", got, refused)
	}
}

func TestPlaceRefusesMainCheckoutFromWorktree(t *testing.T) {
	project, tree := checkout(t)
	in := map[string]any{"file": filepath.Join(project, "internal/plant/plant.go")}
	_, refused := place(in, tree, project)
	if !strings.Contains(refused, "outside this worktree") {
		t.Errorf("refused %q; want a refusal of a path in the main checkout", refused)
	}
}

func TestPlaceKeepsOtherModules(t *testing.T) {
	project, tree := checkout(t)
	other := filepath.Join(filepath.Dir(project), "tools", "a.go")
	in := map[string]any{"file": other}
	if got, refused := place(in, tree, project); got != nil || refused != "" {
		t.Errorf("a module outside the checkout: got %v, refused %q; want the call unchanged", got, refused)
	}
}
