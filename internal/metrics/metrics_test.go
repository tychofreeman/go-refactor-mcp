package metrics

import (
	"reflect"
	"testing"
)

const a = `package p

// Classify buckets n.
func Classify(n int) string {
	if n < 0 {
		return "neg"
	}
	for i := 0; i < n; i++ {
		if i > 10 && n%2 == 0 {
			return "big"
		}
	}
	return "small"
}

type T struct{}

func (t *T) M() {}
`

const b = `package p

func helper() bool { return true }
`

const aTest = `package p

import "testing"

func TestClassify(t *testing.T) {
	if Classify(1) != "small" {
		t.Fatal()
	}
}
`

// TypeScript's functions, methods and functions bound to a const are
// measured as Go's are; test files are left out.
func TestMeasureTypeScript(t *testing.T) {
	funcs, err := Measure(map[string][]byte{
		"src/a.ts": []byte(`/** Waters. */
export function water(n: number): string {
  if (n > 1 && n < 9) {
    for (const x of [1]) {
      if (x) {
        return "a";
      }
    }
  }
  return n > 0 ? "b" : "c";
}

export class Bed {
  depth(): number {
    return 1;
  }
}

export const twice = (n: number) => n * 2;
`),
		"src/a.test.ts": []byte("export function t(): void {}\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Func{
		{File: "src/a.ts", Name: "water", Lines: 11, Complexity: 6, Nesting: 3},
		{File: "src/a.ts", Name: "Bed.depth", Lines: 3, Complexity: 1},
		{File: "src/a.ts", Name: "twice", Lines: 1, Complexity: 1},
	}
	if !reflect.DeepEqual(funcs, want) {
		t.Errorf("Measure =\n%+v\nwant\n%+v", funcs, want)
	}
	if _, err := Measure(map[string][]byte{"src/b.ts": []byte("export function (")}); err == nil {
		t.Error("a file that does not parse was measured")
	}
}

func TestMeasureSkipsTestFiles(t *testing.T) {
	got, err := Measure(map[string][]byte{"b.go": []byte(b), "a.go": []byte(a), "a_test.go": []byte(aTest)})
	if err != nil {
		t.Fatal(err)
	}
	want := []Func{
		{File: "a.go", Name: "Classify", Lines: 12, Complexity: 5, Nesting: 2},
		{File: "a.go", Name: "T.M", Lines: 1, Complexity: 1, Nesting: 0},
		{File: "b.go", Name: "helper", Lines: 1, Complexity: 1, Nesting: 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Measure:\n got %+v\nwant %+v", got, want)
	}
}

func TestMeasureReportsParseErrors(t *testing.T) {
	if _, err := Measure(map[string][]byte{"bad.go": []byte("package p\nfunc {")}); err == nil {
		t.Error("want an error for a file that does not parse")
	}
}

func TestSummarize(t *testing.T) {
	funcs := []Func{
		{Name: "a", Lines: 40, Complexity: 12, Nesting: 4},
		{Name: "b", Lines: 10, Complexity: 3, Nesting: 1},
		{Name: "c", Lines: 5, Complexity: 1, Nesting: 0},
	}
	want := Summary{Funcs: 3, Lines: 55, Complexity: 16, MaxComplexity: 12, MaxNesting: 4, OverComplexity: 1, OverNesting: 1}
	if got := Summarize(funcs); got != want {
		t.Errorf("Summarize:\n got %+v\nwant %+v", got, want)
	}
}

func TestFind(t *testing.T) {
	funcs := []Func{{File: "a.go", Name: "f", Complexity: 2}, {File: "b.go", Name: "f", Complexity: 3}}
	if f, ok := Find(funcs, "b.go", "f"); !ok || f.Complexity != 3 {
		t.Errorf("Find(b.go, f) = %+v, %v", f, ok)
	}
	if _, ok := Find(funcs, "c.go", "f"); ok {
		t.Error("Find found a function in a file that has none")
	}
}
