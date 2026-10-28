package refactor

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// DeclarerOf is what opts.Declare is: a fix that declares stubs for what
// a file uses and nothing declares, which reads the errors it may clear.
type DeclarerOf[W Workspace] interface {
	// Missing reports whether err is a use of a name nothing declares,
	// and returns its file, the name, the key Declared lists it under,
	// and a name the file's code declares that name may misspell ("" if
	// none).
	Missing(ws W, err string) (file, name, key, close string, ok bool)
	// Declared returns the keys of what ws declares. A batch takes them
	// when it begins, so that it does not declare again what a step
	// removed.
	Declared(ws W) map[string]bool
}

// declareMissing runs opts.Declare on each file whose errors errs name
// functions or methods nothing declares, for those names, except a name
// close to one its package declares, which may be misspelled instead, and
// a name its package declared when the batch began (before), which a step
// removed. It returns the steps it applied, and a note on each name it
// left; it calls done on each step just after applying it.
func declareMissing[W Workspace](ws W, errs []string, before map[string]bool, opts OptionsOf[W], done func(*Result)) ([]*Result, []string, error) {
	d, ok := any(opts.Declare).(DeclarerOf[W])
	if !ok {
		return nil, nil, nil
	}
	byFile := map[string][]string{}
	var files, skipped []string
	for _, e := range errs {
		file, name, key, close, ok := d.Missing(ws, e)
		if !ok {
			continue
		}
		if close != "" {
			skipped = append(skipped, fmt.Sprintf("%s was not declared for you, since it is close to %s; if it is meant to be new, declare it with create_from_usage", name, close))
			continue
		}
		if before[key] {
			if note := fmt.Sprintf("%s was not declared for you, since an earlier step removed it; change what still uses it", name); !slices.Contains(skipped, note) {
				skipped = append(skipped, note)
			}
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
		done(res)
	}
	return declared, skipped, nil
}

// ImportFixer is what opts.Imports is: a fix of a file's imports, which
// names the errors it may clear.
type ImportFixer interface {
	// Clears returns the file err is in, when adding or removing an
	// import may clear it.
	Clears(err string) (file string, ok bool)
}

// fixImports runs opts.Imports on each file whose errors errs may be
// imports to add or remove, and returns the steps it applied; it calls
// done on each step just after applying it.
func fixImports[W Workspace](ws W, errs []string, opts OptionsOf[W], done func(*Result)) ([]*Result, error) {
	fixer, ok := any(opts.Imports).(ImportFixer)
	if !ok {
		return nil, nil
	}
	var files []string
	for _, e := range errs {
		if file, ok := fixer.Clears(e); ok && !slices.Contains(files, file) {
			files = append(files, file)
		}
	}
	var fixed []*Result
	for _, file := range files {
		args, err := json.Marshal(map[string]any{"file": file})
		if err != nil {
			return fixed, err
		}
		res, err := Run(ws, opts.Imports, args, OptionsOf[W]{Compact: opts.Compact, Unchecked: true})
		if err != nil {
			return fixed, err
		}
		if res.Applied {
			fixed = append(fixed, res)
			done(res)
		}
	}
	return fixed, nil
}

// notCalled is opts.Declare's refusal of a name the file does not call,
// such as an undefined variable.
var notCalled = regexp.MustCompile(`calls no undeclared function (\w+)$`)

// untypeable is opts.Declare's refusal of a name whose stub it cannot
// type from how the file uses it: a function's results, a field's type,
// or a new type a keyed literal gives a field the type of.
var untypeable = regexp.MustCompile(`^cannot tell the (?:results of (\w+) from|type of field (\w+) of \w+ from how|type of field \w+ of (\w+) from)`)

// declareIn runs opts.Declare on file for names, leaving out each name it
// refuses as not called. It returns the step, if one applied, and the
// names it declared; and why it declared none, or left some out.
func declareIn[W Workspace](ws W, file string, names []string, opts OptionsOf[W]) (*Result, []string, string, error) {
	var dropped []string // names opts.Declare refused as not called
	var untyped []string // why each name opts.Declare could not type has no stub
	why := func() string {
		notes := slices.Clone(untyped)
		if len(dropped) > 0 {
			notes = append(notes, fmt.Sprintf("no stub was declared for %s, which %s not called (a stub is for a function or method called, or passed where a function is needed, or a type made with a literal)", orList(dropped, "and"), map[bool]string{true: "is", false: "are"}[len(dropped) == 1]))
		}
		return strings.Join(notes, "\n")
	}
	for len(names) > 0 {
		args, err := json.Marshal(map[string]any{"file": file, "names": names})
		if err != nil {
			return nil, nil, "", err
		}
		res, err := Run(ws, opts.Declare, args, OptionsOf[W]{Compact: opts.Compact, Unchecked: true})
		if err != nil {
			return res, names, "", err
		}
		if res.Applied {
			return res, names, why(), nil
		}
		// A name it refuses does not keep the others from their stubs.
		name := ""
		if m := notCalled.FindStringSubmatch(res.Refused); m != nil {
			name = m[1]
			dropped = append(dropped, name)
		} else if m := untypeable.FindStringSubmatch(res.Refused); m != nil {
			name = m[1] + m[2] + m[3]
			untyped = append(untyped, fmt.Sprintf("no stub was declared for %s: %s", name, res.Refused))
		}
		if !slices.Contains(names, name) {
			return nil, nil, fmt.Sprintf("no stubs were declared for %s: %s", orList(names, "and"), res.Refused), nil
		}
		names = slices.DeleteFunc(slices.Clone(names), func(n string) bool { return n == name })
	}
	return nil, nil, why(), nil
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
