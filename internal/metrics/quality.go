package metrics

import (
	"go/parser"
	"go/token"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/inspect"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
)

// Changed returns the functions of after (path -> source) that are new,
// or whose text differs from that of the function of the same file and
// name in before.
func Changed(before, after map[string][]byte) ([]Func, error) {
	old := funcTexts(before)
	funcs, err := Measure(after)
	if err != nil {
		return nil, err
	}
	now := funcTexts(after)
	var out []Func
	for _, f := range funcs {
		k := [2]string{f.File, f.Name}
		if t, ok := old[k]; !ok || t != now[k] {
			out = append(out, f)
		}
	}
	return out, nil
}

// DupWindow is how many consecutive significant lines (not blank, a
// comment or only brackets) make a duplicate.
const DupWindow = 4

// Duplicated counts the significant lines of files, test files left
// out, that are part of a run of DupWindow significant lines that
// appears more than once.
func Duplicated(files map[string][]byte) int {
	type at struct {
		file string
		i    int // index among the file's significant lines
	}
	starts := map[string][]at{} // a run of lines -> where it starts
	for p, src := range files {
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			continue
		}
		lines := significantLines(src)
		for i := 0; i+DupWindow <= len(lines); i++ {
			k := strings.Join(lines[i:i+DupWindow], "\n")
			starts[k] = append(starts[k], at{p, i})
		}
	}
	dup := map[at]bool{}
	for _, ss := range starts {
		if len(ss) < 2 {
			continue
		}
		for _, s := range ss {
			for j := range DupWindow {
				dup[at{s.file, s.i + j}] = true
			}
		}
	}
	return len(dup)
}

// funcTexts maps each function of files, by file and name, to its
// source. Files that do not parse are left out.
func funcTexts(files map[string][]byte) map[[2]string]string {
	out := map[[2]string]string{}
	for p, src := range files {
		if isTypeScript(p) {
			for _, m := range tsapi.Measure(p, src) {
				out[[2]string{p, m.Name}] = m.Text
			}
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, src, parser.ParseComments)
		if err != nil {
			continue
		}
		lines := strings.SplitAfter(string(src), "\n")
		for _, d := range inspect.Outline(fset, f) {
			if d.Kind == "func" || d.Kind == "method" {
				out[[2]string{p, d.Name}] = strings.Join(lines[d.Start-1:d.End], "")
			}
		}
	}
	return out
}

// significantLines returns the lines of src that say something, trimmed:
// not blank, a comment, only brackets, the package clause or imports.
func significantLines(src []byte) []string {
	var out []string
	inImports := false
	for _, l := range strings.Split(string(src), "\n") {
		l = strings.TrimSpace(l)
		switch {
		case inImports:
			inImports = l != ")"
		case l == "import (":
			inImports = true
		case l == "", strings.HasPrefix(l, "//"), strings.HasPrefix(l, "package "), strings.HasPrefix(l, "import "),
			strings.Trim(l, "{}()[];,") == "":
		default:
			out = append(out, l)
		}
	}
	return out
}
