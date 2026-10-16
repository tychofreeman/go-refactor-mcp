package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
)

// benchModule writes a module of n packages in a chain (p0 <- p1 <- ...),
// each with a few files that use the standard library and the previous
// package, plus a test file.
func benchModule(tb testing.TB, n int) string {
	tb.Helper()
	dir := tb.TempDir()
	write := func(name, src string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			tb.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	write("go.mod", "module example.com/bench\n\ngo 1.22\n")
	for i := 0; i < n; i++ {
		pkg := fmt.Sprintf("p%d", i)
		dep := ""
		use := "0"
		if i > 0 {
			dep = fmt.Sprintf("\t\"example.com/bench/p%d\"\n", i-1)
			use = fmt.Sprintf("p%d.Value(n)", i-1)
		}
		write(pkg+"/a.go", fmt.Sprintf(`package %s

import (
	"fmt"
	"strings"
%s)

// Value computes a value from n.
func Value(n int) int {
	total := n * 2
	return total + %s
}

func Describe(n int) string {
	return strings.ToUpper(fmt.Sprintf("value %%d", Value(n)))
}
`, pkg, dep, use))
		write(pkg+"/b.go", fmt.Sprintf(`package %s

import "sort"

type Pair struct{ A, B int }

func Sorted(xs []int) []int {
	out := append([]int(nil), xs...)
	sort.Ints(out)
	return out
}
`, pkg))
		write(pkg+"/a_test.go", fmt.Sprintf(`package %s

import "testing"

func TestValue(t *testing.T) {
	if Value(1) < 2 {
		t.Fatal("bad")
	}
}
`, pkg))
	}
	return dir
}

// BenchmarkApplyUndo measures one edit-and-undo cycle in the package at
// the bottom of the chain, which every other package depends on: the
// reload work behind each applied refactoring.
func BenchmarkApplyUndo(b *testing.B) {
	b.Setenv("GOWORK", "off")
	dir := benchModule(b, 20)
	w, err := Load(dir)
	if err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(dir, "p0", "a.go")
	src, _ := os.ReadFile(path)
	i := strings.Index(string(src), "total := n * 2")
	e := []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: i, End: i + len("total"), NewText: "sum"}, {
		Start: strings.Index(string(src), "return total"), End: strings.Index(string(src), "return total") + len("return total"), NewText: "return sum",
	}}}}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if _, err := w.Apply("bench", e); err != nil {
			b.Fatal(err)
		}
		if _, err := w.Undo(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLoad measures a full load of the same module, for comparison.
func BenchmarkLoad(b *testing.B) {
	b.Setenv("GOWORK", "off")
	dir := benchModule(b, 20)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if _, err := Load(dir); err != nil {
			b.Fatal(err)
		}
	}
}
