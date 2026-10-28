// Package createfromusage is create_from_usage for TypeScript: it
// declares stubs for what a file calls and nothing declares, typed from
// the calls. The engine runs it in a batch checked at its end, so that a
// test can be written before what it calls.
package createfromusage

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// CreateFromUsage declares stubs.
type CreateFromUsage struct{}

// New returns create_from_usage.
func New() *CreateFromUsage { return &CreateFromUsage{} }

func (*CreateFromUsage) Name() string { return "create_from_usage" }

func (*CreateFromUsage) Description() string {
	return `Declare stubs for functions and methods a file calls that nothing declares, typed from the calls: an edit, so that a test can be written first. With a test calling shade(10) where const shaded: number = shade(10), it declares function shade(n: number): number { throw new Error("not implemented"); }; in the module the file imports shade from, exported, if it does; a method in the class (a signature in the interface) it is called on. A batch checked at its end declares them for you.
Refuses, saying why: a name the file does not call; a call whose result type its place does not tell.`
}

func (*CreateFromUsage) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":  {"type": "string", "description": "The file that calls them, relative to the root."},
    "names": {"type": "array", "items": {"type": "string"}, "description": "The names it calls that nothing declares."}
  },
  "required": ["file", "names"],
  "additionalProperties": false
}`)
}

func (*CreateFromUsage) ChangesBehavior() {}

func (*CreateFromUsage) Fixes() {}

// The errors of a call of a function or method nothing declares, with the
// name the compiler suggests instead, if any.
var (
	cannotFind = regexp.MustCompile(`^(.+\.(?:ts|tsx|mts|cts)):\d+:\d+: Cannot find name '([\w$]+)'\.(?: Did you mean '([\w$]+)'\?)?`)
	noExport   = regexp.MustCompile(`^(.+\.(?:ts|tsx|mts|cts)):\d+:\d+: Module '.*' has no exported member '([\w$]+)'\.(?: Did you mean '([\w$]+)'\?)?`)
	noProperty = regexp.MustCompile(`^(.+\.(?:ts|tsx|mts|cts)):\d+:\d+: Property '([\w$]+)' does not exist on type '([\w$]+)'\.(?: Did you mean '([\w$]+)'\?)?`)
)

// Missing reports whether err is a call of a name nothing declares, and
// which (see refactor.DeclarerOf): its key is "file\x00name" for a name at
// the top level of a file, the module a test imports it from for an
// import, and "Type.name" for a member.
func (*CreateFromUsage) Missing(ws *workspace.Workspace, err string) (file, name, key, close string, ok bool) {
	if m := cannotFind.FindStringSubmatch(err); m != nil {
		return m[1], m[2], m[1] + "\x00" + m[2], m[3], true
	}
	if m := noExport.FindStringSubmatch(err); m != nil {
		key = m[1] + "\x00" + m[2]
		if from, ok := ws.ImportedFrom(ws.Abs(m[1]), m[2]); ok && from != "" {
			key = ws.Rel(from) + "\x00" + m[2]
		}
		return m[1], m[2], key, m[3], true
	}
	if m := noProperty.FindStringSubmatch(err); m != nil {
		return m[1], m[2], m[3] + "." + m[2], m[4], true
	}
	return "", "", "", "", false
}

// Declared returns what ws declares: "file\x00name" for each declaration
// at the top level of each file, "Class.member" for each method of a
// class.
func (*CreateFromUsage) Declared(ws *workspace.Workspace) map[string]bool {
	out := map[string]bool{}
	for _, f := range ws.Files() {
		for _, d := range ws.Outline(f) {
			if strings.Contains(d.Name, ".") {
				out[d.Name] = true
			} else {
				out[ws.Rel(f)+"\x00"+d.Name] = true
			}
		}
	}
	return out
}

func (*CreateFromUsage) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File  string   `json:"file"`
		Names []string `json:"names"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", a.File)
	}
	if len(a.Names) == 0 {
		return nil, refactor.Refusef("give the names to declare")
	}
	stubs, why := ws.Stubs(path, a.Names)
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	// Stubs going to the same place go in one edit.
	type place struct{ file, owner string }
	var order []place
	byPlace := map[place][]tsapi.Stub{}
	for _, s := range stubs {
		k := place{s.File, s.Owner}
		if byPlace[k] == nil {
			order = append(order, k)
		}
		byPlace[k] = append(byPlace[k], s)
	}
	byFile := map[string][]edit.TextEdit{}
	var files []string
	plan := &refactor.Plan{}
	for _, k := range order {
		var texts []string
		for _, s := range byPlace[k] {
			texts = append(texts, s.Text)
			where := ws.Rel(s.File)
			if s.Owner != "" {
				where = s.Owner + " in " + where
			}
			plan.Choices = append(plan.Choices, fmt.Sprintf("%s, in %s", strings.SplitN(s.Text, " {", 2)[0], where))
		}
		var e edit.TextEdit
		if k.owner == "" {
			src, err := os.ReadFile(k.file)
			if err != nil {
				return nil, err
			}
			lead := "\n"
			if len(src) > 0 && src[len(src)-1] != '\n' {
				lead = "\n\n"
			}
			e = edit.TextEdit{Start: len(src), End: len(src), NewText: lead + strings.Join(texts, "\n\n") + "\n"}
		} else {
			pl, _, why := ws.MemberPlace(k.file, k.owner, "", "")
			if why != "" {
				return nil, refactor.Refusef("%s", why)
			}
			e = edit.TextEdit{Start: pl.At, End: pl.End, NewText: pl.Prefix + gocode.Reindent(strings.Join(texts, "\n"), pl.Indent, false) + pl.Suffix}
		}
		if byFile[k.file] == nil {
			files = append(files, k.file)
		}
		byFile[k.file] = append(byFile[k.file], e)
	}
	sort.Strings(files)
	for _, f := range files {
		plan.Edits = append(plan.Edits, edit.FileEdit{Path: f, Edits: byFile[f]})
	}
	var names []string
	for _, s := range stubs {
		if s.Owner != "" {
			names = append(names, s.Owner+"."+s.Name)
		} else {
			names = append(names, s.Name)
		}
	}
	plan.Summary = "declared stubs for " + strings.Join(names, ", ")
	return plan, nil
}
