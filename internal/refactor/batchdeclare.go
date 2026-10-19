package refactor

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// The errors of a call of a function or method nothing declares.
var (
	undefinedName = regexp.MustCompile(`^(.+\.go):\d+:\d+: undefined: (?:(\w+)\.)?(\w+)$`)
	missingMethod = regexp.MustCompile(`^(.+\.go):\d+:\d+: .* undefined \(type .* has no field or method (\w+)\)$`)
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
		if res == nil {
			if why != "" {
				skipped = append(skipped, why)
			}
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

// notCalled is opts.Declare's refusal of a name the file does not call,
// such as an undefined variable.
var notCalled = regexp.MustCompile(`calls no undeclared function (\w+)$`)

// declareIn runs opts.Declare on file for names, leaving out each name it
// refuses as not called. It returns the step, if one applied, and the
// names it declared, or else why it declared none.
func declareIn(ws *workspace.Workspace, file string, names []string, opts Options) (*Result, []string, string, error) {
	for len(names) > 0 {
		args, err := json.Marshal(map[string]any{"file": file, "names": names})
		if err != nil {
			return nil, nil, "", err
		}
		res, err := Run(ws, opts.Declare, args, Options{Compact: opts.Compact, Unchecked: true})
		if err != nil || res.Applied {
			return res, names, "", err
		}
		m := notCalled.FindStringSubmatch(res.Refused)
		if m == nil || !slices.Contains(names, m[1]) {
			return nil, nil, fmt.Sprintf("no stubs were declared for %s: %s", orList(names, "and"), res.Refused), nil
		}
		names = slices.DeleteFunc(slices.Clone(names), func(n string) bool { return n == m[1] })
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
		if n != name && (strings.EqualFold(n, name) || len(name) >= 4 && editDistance(n, name) <= 1) {
			return n
		}
	}
	return ""
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
