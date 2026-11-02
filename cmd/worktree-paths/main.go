// Command worktree-paths is a Claude Code hook (PreToolUse, on
// gorefactor's tools) that sends a call made from a git worktree to that
// worktree. The MCP server is bound to the checkout its session started
// in, and a subagent with worktree isolation, or a session that moved
// into a worktree, shares it; the hook's input has the caller's own
// directory. Relative paths become absolute paths in the caller's
// worktree, and a path into the main checkout from a worktree is refused,
// as Claude Code refuses git there.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/lang"
)

// call is the part of a hook's input worktree-paths looks at.
type call struct {
	ToolInput map[string]any `json:"tool_input"`
	Cwd       string         `json:"cwd"`
}

// answer is a PreToolUse hook's output: the call's arguments replaced,
// or the call denied with the reason Claude Code shows the model.
type answer struct {
	HookSpecificOutput struct {
		HookEventName            string         `json:"hookEventName"`
		PermissionDecision       string         `json:"permissionDecision,omitempty"`
		PermissionDecisionReason string         `json:"permissionDecisionReason,omitempty"`
		UpdatedInput             map[string]any `json:"updatedInput,omitempty"`
	} `json:"hookSpecificOutput"`
}

func main() {
	var c call
	if err := json.NewDecoder(os.Stdin).Decode(&c); err != nil || c.Cwd == "" {
		return // not a call to place
	}
	updated, refused := place(c.ToolInput, c.Cwd, os.Getenv("CLAUDE_PROJECT_DIR"))
	if updated == nil && refused == "" {
		return
	}
	// No permission decision with the new arguments: the call goes on
	// through the user's permission rules. A denial is on stdout with exit
	// status 0, as read-guard's is.
	var out answer
	out.HookSpecificOutput.HookEventName = "PreToolUse"
	if refused != "" {
		out.HookSpecificOutput.PermissionDecision = "deny"
		out.HookSpecificOutput.PermissionDecisionReason = refused
	} else {
		out.HookSpecificOutput.UpdatedInput = updated
	}
	json.NewEncoder(os.Stdout).Encode(out)
}

// place returns in, the arguments of a gorefactor call made from cwd,
// with its relative paths made absolute in cwd's worktree; nil when cwd
// is not in a worktree other than project, the main checkout, or no path
// changes. A path into project outside the worktree is refused instead.
func place(in map[string]any, cwd string, project string) (map[string]any, string) {
	tree := worktree(cwd)
	if tree == "" || tree == filepath.Clean(project) {
		return nil, ""
	}
	changed := false
	var refused string
	out := lang.MapFiles(in, func(p string) string {
		if filepath.IsAbs(p) {
			p = filepath.Clean(p)
			if inside(p, project) && !inside(p, tree) && refused == "" {
				refused = fmt.Sprintf("%s is in the main checkout, outside this worktree (%s); give paths relative to the worktree's root.", p, tree)
			}
			return p
		}
		changed = true
		if a := filepath.Join(tree, p); lang.Exists(a) || !lang.Exists(filepath.Join(cwd, p)) {
			return a
		}
		return filepath.Join(cwd, p)
	})
	if refused != "" || !changed {
		return nil, refused
	}
	return out.(map[string]any), ""
}

// worktree is the root of the git working tree holding dir: the nearest
// directory at or above it with a .git entry, or "".
func worktree(dir string) string {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if lang.Exists(filepath.Join(d, ".git")) {
			return d
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

// inside reports whether path is dir or under it.
func inside(path, dir string) bool {
	r, err := filepath.Rel(filepath.Clean(dir), path)
	return err == nil && r != ".." && !strings.HasPrefix(r, "../")
}
