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
	return `Declare stubs for what a file uses that nothing declares, typed from the uses: an edit, so that a test can be written first. With a test calling shade(10) where const shaded: number = shade(10), it declares function shade(n: number): number { throw new Error("not implemented"); }; in the module the file imports shade from, exported, if it does. new Pot(3) declares class Pot { constructor(n: number) {} }; a method called on a class (or interface) goes in it, and a property read or written there becomes size!: number. results gives types the uses do not tell; to_file puts the file's own stubs in another file. A batch checked at its end declares them for you.
Refuses, saying why: a name the file does not call, make or read; a use whose type its place does not tell (give results).`
}

func (*CreateFromUsage) InputSchema() json.RawMessage {
	return refactor.Schema[createFromUsageArgs]()
}

// createFromUsageArgs are the arguments of create_from_usage.
type createFromUsageArgs struct {
	File    string            `json:"file" desc:"The file that calls them, relative to the root."`
	Names   []string          `json:"names" desc:"The names it uses that nothing declares."`
	Results map[string]string `json:"results,omitempty" desc:"Optional: types by name, for uses that do not tell them: a function's or method's result, a property's type ({\"shade\": \"number\"})."`
	ToFile  string            `json:"to_file,omitempty" desc:"Optional: the file, relative to the root, for the stubs of the file's own names, exported and imported; created if new."`
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
	var a createFromUsageArgs
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
	stubs, why := ws.Stubs(path, a.Names, a.Results)
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	// to_file takes the stubs of the file's own names, exported, and the
	// file imports them.
	var moved []tsapi.Import
	if a.ToFile != "" {
		to := ws.Abs(a.ToFile)
		for i, s := range stubs {
			if s.Owner == "" && s.File == path {
				stubs[i].File, stubs[i].Text, stubs[i].Before = to, "export "+s.Text, -1
				moved = append(moved, tsapi.Import{Name: s.Name, From: to})
			}
		}
	}
	// Stubs going to the same place go in one edit.
	type place struct {
		file, owner string
		before      int
	}
	var order []place
	byPlace := map[place][]tsapi.Stub{}
	for _, s := range stubs {
		k := place{s.File, s.Owner, s.Before}
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
		switch {
		case k.before >= 0:
			e = edit.TextEdit{Start: k.before, End: k.before, NewText: strings.Join(texts, "\n\n") + "\n\n"}
		case k.owner == "":
			src, err := os.ReadFile(k.file)
			if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			lead := "\n"
			switch {
			case len(src) == 0:
				lead = ""
			case src[len(src)-1] != '\n':
				lead = "\n\n"
			}
			e = edit.TextEdit{Start: len(src), End: len(src), NewText: lead + strings.Join(texts, "\n\n") + "\n"}
		default:
			_, p, why := ws.Project(k.file)
			var pl tsapi.Place
			if why == "" {
				pl, _, why = p.MemberPlace(k.file, k.owner, "", "")
			}
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
	if len(moved) > 0 {
		imports, why := ws.AddImports(path, moved)
		if why != "" {
			return nil, refactor.Refusef("%s", why)
		}
		if byFile[path] == nil {
			files = append(files, path)
		}
		byFile[path] = append(byFile[path], imports.Edits...)
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
