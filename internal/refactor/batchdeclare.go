package refactor

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// The errors of a use of a function, method, type or field nothing
// declares.
var (
	undefinedName = regexp.MustCompile(`^(.+\.go):\d+:\d+: undefined: (?:(\w+)\.)?(\w+)$`)
	missingMethod = regexp.MustCompile(`^(.+\.go):\d+:\d+: .* undefined \(type .* has no field or method (\w+)\)$`)
	unknownField  = regexp.MustCompile(`^(.+\.go):\d+:\d+: unknown field (\w+) in struct literal`)
)

// declareMissing runs opts.Declare on each file whose errors errs name
// functions or methods nothing declares, for those names, except a name
// close to one its package declares, which may be misspelled instead. It
// returns the steps it applied, and a note on each name it left.
func declareMissing(ws *workspace.Workspace, errs []string, opts Options) ([]*Result, []string, error) {
	byFile := map[string][]string{}
	var files, skipped []string
	for _, e := range errs {
		var file, name string
		if m := undefinedName.FindStringSubmatch(e); m != nil {
			file, name = m[1], m[3]
			if close := closeName(ws, file, name); m[2] == "" && close != "" {
				skipped = append(skipped, fmt.Sprintf("%s was not declared for you, since it is close to %s; if it is meant to be new, declare it with create_from_usage", name, close))
				continue
			}
		} else if m := missingMethod.FindStringSubmatch(e); m != nil {
			file, name = m[1], m[2]
		} else if m := unknownField.FindStringSubmatch(e); m != nil {
			file, name = m[1], m[2]
		} else {
			continue
		}
		if _, ok := byFile[file]; !ok {
			files = append(files, file)
		}
		if !slices.Contains(byFile[file], name) {
			byFile[file] = append(byFile[file], name)
		}
	}
	var declared []*Result
	for _, file := range files {
		res, names, why, err := declareIn(ws, file, byFile[file], opts)
		if err != nil {
			return declared, skipped, err
		}
		if why != "" {
			skipped = append(skipped, why)
		}
		if res == nil {
			continue
		}
		it, check := "it", "it is"
		if len(names) > 1 {
			it, check = "them", "each is"
		}
		res.Notes = append(res.Notes, fmt.Sprintf("added by the batch, since the steps call %s and nothing declares %s; check that %s meant to be new, not a misspelled name", orList(names, "and"), it, check))
		declared = append(declared, res)
	}
	return declared, skipped, nil
}

// importError is an error an import fix may clear: an import nothing
// uses, or a name that may be a package not imported.
var importError = regexp.MustCompile(`^(.+\.go):\d+:\d+: (".+" imported (as \w+ )?and not used|undefined: \w+)$`)

// fixImports runs opts.Imports on each file whose errors errs may be
// imports to add or remove, and returns the steps it applied.
func fixImports(ws *workspace.Workspace, errs []string, opts Options) ([]*Result, error) {
	var files []string
	for _, e := range errs {
		if m := importError.FindStringSubmatch(e); m != nil && !slices.Contains(files, m[1]) {
			files = append(files, m[1])
		}
	}
	var fixed []*Result
	for _, file := range files {
		args, err := json.Marshal(map[string]any{"file": file})
		if err != nil {
			return fixed, err
		}
		res, err := Run(ws, opts.Imports, args, Options{Compact: opts.Compact, Unchecked: true})
		if err != nil {
			return fixed, err
		}
		if res.Applied {
			fixed = append(fixed, res)
		}
	}
	return fixed, nil
}

// notCalled is opts.Declare's refusal of a name the file does not call,
// such as an undefined variable.
var notCalled = regexp.MustCompile(`calls no undeclared function (\w+)$`)

// declareIn runs opts.Declare on file for names, leaving out each name it
// refuses as not called. It returns the step, if one applied, and the
// names it declared; and why it declared none, or left some out.
func declareIn(ws *workspace.Workspace, file string, names []string, opts Options) (*Result, []string, string, error) {
	var dropped []string // names opts.Declare refused as not called
	notCalledNote := func() string {
		return fmt.Sprintf("no stub was declared for %s, which %s not called (a stub is for a function or method called, or passed where a function is needed, or a type made with a literal)", orList(dropped, "and"), map[bool]string{true: "is", false: "are"}[len(dropped) == 1])
	}
	for len(names) > 0 {
		args, err := json.Marshal(map[string]any{"file": file, "names": names})
		if err != nil {
			return nil, nil, "", err
		}
		res, err := Run(ws, opts.Declare, args, Options{Compact: opts.Compact, Unchecked: true})
		if err != nil {
			return res, names, "", err
		}
		if res.Applied && len(dropped) > 0 {
			return res, names, notCalledNote(), nil
		}
		if res.Applied {
			return res, names, "", nil
		}
		m := notCalled.FindStringSubmatch(res.Refused)
		if m == nil || !slices.Contains(names, m[1]) {
			return nil, nil, fmt.Sprintf("no stubs were declared for %s: %s", orList(names, "and"), res.Refused), nil
		}
		dropped = append(dropped, m[1])
		names = slices.DeleteFunc(slices.Clone(names), func(n string) bool { return n == m[1] })
	}
	if len(dropped) > 0 {
		return nil, nil, notCalledNote(), nil
	}
	return nil, nil, "", nil
}

// closeName returns a name file's package declares that name may be a
// misspelling of, or "".
func closeName(ws *workspace.Workspace, file, name string) string {
	pkg, _ := ws.FileSyntax(ws.Abs(file))
	if pkg == nil || pkg.Types == nil {
		return ""
	}
	for _, n := range pkg.Types.Scope().Names() {
		if Close(n, name) {
			return n
		}
	}
	return ""
}

// Close reports whether name, which is not n, may be a misspelling of
// it: the same but for case, or for one letter in a name of four or more.
func Close(n, name string) bool {
	return n != name && (strings.EqualFold(n, name) || len(name) >= 4 && editDistance(n, name) <= 1)
}

// editDistance is the edit distance between a and b, counting two
// adjacent letters swapped as one edit (Scael for Scale).
func editDistance(a, b string) int {
	// d[i][j] is the distance between a[:i] and b[:j].
	d := make([][]int, len(a)+1)
	for i := range d {
		d[i] = make([]int, len(b)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1) // two letters swapped
			}
		}
	}
	return d[len(a)][len(b)]
}
