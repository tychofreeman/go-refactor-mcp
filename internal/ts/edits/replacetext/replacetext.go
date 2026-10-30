// Package replacetext is replace_text for a TypeScript solution: an edit
// of exact text in any file under the root.
package replacetext

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// ReplaceText replaces exact text.
type ReplaceText struct{}

// New returns replace_text.
func New() *ReplaceText { return &ReplaceText{} }

func (*ReplaceText) ChangesBehavior() {}

func (*ReplaceText) Name() string { return "replace_text" }

func (*ReplaceText) Description() string {
	return `Replace exact text in a file: an edit, for what the other tools do not cover: string literals, comments, and files that are not source (package.json, fixtures, docs). old "version 1.2" and new "version 1.3" change that text in README.md.
old must be as in the file and occur once, or set all_occurrences; spacing within lines may differ where that still finds it. function limits the search to one function of a source file. append with no old adds new at the end of the file. files in place of file makes the same replacement in several files (old must be in each). It addresses text, not lines, so it stays valid after earlier steps of a batch.`
}

func (*ReplaceText) InputSchema() json.RawMessage {
	return refactor.Schema[replaceTextArgs]()
}

// replaceTextArgs are the arguments of replace_text.
type replaceTextArgs struct {
	File     string   `json:"file,omitempty" desc:"Path of the file, relative to the root: any file under it, source or not."`
	Files    []string `json:"files,omitempty" desc:"Instead of file: several files, to make the same replacement in each."`
	Old      string   `json:"old,omitempty" desc:"The text to replace, as it is in the file."`
	New      string   `json:"new" desc:"The text to put in its place."`
	All      bool     `json:"all_occurrences,omitempty" desc:"Replace every occurrence of old in the file, not just the only one."`
	Function string   `json:"function,omitempty" desc:"Optional: look for old only in this function of the file (name, or Class.method)."`
	Append   bool     `json:"append,omitempty" desc:"Instead of old: add new at the end of the file."`
}

func (*ReplaceText) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a replaceTextArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	files := a.Files
	switch {
	case a.File != "" && len(a.Files) > 0:
		return nil, refactor.Refusef("give file or files, not both")
	case a.File != "":
		files = []string{a.File}
	case len(files) == 0:
		return nil, refactor.Refusef("give file, or files")
	}
	switch {
	case a.Append && (a.Old != "" || a.Function != ""):
		return nil, refactor.Refusef("append adds new at the end; leave out old and function")
	case !a.Append && a.Old == "":
		return nil, refactor.Refusef("old is empty; give the text to replace, or append to add new at the end")
	case a.Function != "" && len(files) > 1:
		return nil, refactor.Refusef("function names a function of one file; give file, not files")
	}
	var fes []edit.FileEdit
	n := 0
	loose := false
	for _, f := range files {
		path := ws.Abs(f)
		if !ws.Contains(path) {
			return nil, refactor.Refusef("%s is outside the root %s, or in node_modules", f, ws.Root)
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, refactor.Refusef("%s: no such file", f)
		}
		text := string(src)
		if a.Append {
			add := a.New
			if text != "" && !strings.HasSuffix(text, "\n") {
				add = "\n" + add
			}
			fes = append(fes, edit.FileEdit{Path: path, Edits: []edit.TextEdit{{Start: len(text), End: len(text), NewText: add}}})
			n++
			continue
		}
		lo, hi := 0, len(text)
		if a.Function != "" {
			var why string
			if lo, hi, why = ws.FunctionSpan(path, a.Function); why != "" {
				return nil, refactor.Refusef("%s", why)
			}
		}
		spans, l, why := textutil.Locate(text, a.Old, lo, hi, a.All, f)
		if why != "" {
			return nil, refactor.Refusef("%s", why)
		}
		loose = loose || l
		var tes []edit.TextEdit
		for _, s := range spans {
			tes = append(tes, edit.TextEdit{Start: s[0], End: s[1], NewText: a.New})
		}
		n += len(tes)
		fes = append(fes, edit.FileEdit{Path: path, Edits: tes})
	}
	in := ws.Rel(ws.Abs(files[0]))
	if len(files) > 1 {
		in = fmt.Sprintf("%d files", len(files))
	}
	plan := &refactor.Plan{Summary: fmt.Sprintf("replaced %d occurrence%s of the text in %s", n, textutil.Plural(n), in), Edits: fes}
	if a.Append {
		plan.Summary = "appended the text to " + in
	}
	if loose {
		plan.Notes = append(plan.Notes, "old matched with the spacing within lines ignored")
	}
	return plan, nil
}
