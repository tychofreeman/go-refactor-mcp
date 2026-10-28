// Package metrics measures the functions in a set of Go and TypeScript
// files, so a
// refactoring run can be judged by numbers: did complexity and nesting go
// down? The numbers are the ones inspect shows the model.
package metrics

import (
	"fmt"
	"go/parser"
	"go/token"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/inspect"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
)

// Thresholds above which a function counts as too complex or too nested.
const (
	ComplexityThreshold = 10
	NestingThreshold    = 3
)

// Func is one function or method.
type Func struct {
	File       string
	Name       string // "Name" or "Type.Method"
	Lines      int    // including the doc comment
	Complexity int
	Nesting    int
}

// Measure returns the functions in files (path -> source), sorted by file
// and position. Test files are skipped: refactorings target the code
// under test.
func Measure(files map[string][]byte) ([]Func, error) {
	var paths []string
	for p := range files {
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") || isTypeScript(p) {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	var out []Func
	for _, p := range paths {
		if isTypeScript(p) {
			ms := tsapi.Measure(p, files[p])
			if ms == nil && !tsapi.Parses(p, files[p]) {
				return nil, fmt.Errorf("%s does not parse", p)
			}
			for _, m := range ms {
				out = append(out, Func{File: p, Name: m.Name, Lines: m.End - m.Start + 1, Complexity: m.Complexity, Nesting: m.Nesting})
			}
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, files[p], parser.ParseComments)
		if err != nil {
			return nil, err
		}
		for _, d := range inspect.Outline(fset, f) {
			if d.Kind != "func" && d.Kind != "method" {
				continue
			}
			out = append(out, Func{File: p, Name: d.Name, Lines: d.End - d.Start + 1, Complexity: d.Complexity, Nesting: d.Nesting})
		}
	}
	return out, nil
}

// isTypeScript reports whether p is a TypeScript source file other than a
// test or a declaration file.
func isTypeScript(p string) bool {
	if strings.HasSuffix(p, ".d.ts") || strings.Contains(p, ".test.") || strings.Contains(p, ".spec.") {
		return false
	}
	return strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".tsx")
}

// Summary aggregates a set of functions.
type Summary struct {
	Funcs          int
	Lines          int
	Complexity     int // total
	MaxComplexity  int
	MaxNesting     int
	OverComplexity int // functions above ComplexityThreshold
	OverNesting    int // functions above NestingThreshold
}

// Summarize aggregates funcs.
func Summarize(funcs []Func) Summary {
	var s Summary
	for _, f := range funcs {
		s.Funcs++
		s.Lines += f.Lines
		s.Complexity += f.Complexity
		s.MaxComplexity = max(s.MaxComplexity, f.Complexity)
		s.MaxNesting = max(s.MaxNesting, f.Nesting)
		if f.Complexity > ComplexityThreshold {
			s.OverComplexity++
		}
		if f.Nesting > NestingThreshold {
			s.OverNesting++
		}
	}
	return s
}

// Find returns the function named name in file.
func Find(funcs []Func, file, name string) (Func, bool) {
	for _, f := range funcs {
		if f.File == file && f.Name == name {
			return f, true
		}
	}
	return Func{}, false
}
