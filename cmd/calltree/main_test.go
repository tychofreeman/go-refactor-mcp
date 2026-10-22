package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTree(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.22\n",
		"a/a.go": `// Package a is shapes.
package a

import "example.com/m/b"

// Shape has an area.
type Shape interface{ Area() float64 }

// Sq is a square.
type Sq struct{ S float64 }

// Area is the square's area.
// More text.
func (q Sq) Area() float64 { return b.Mul(q.S, q.S) }

// Total adds the areas of shapes.
func Total(ss []Shape) float64 {
	t := 0.0
	for _, s := range ss {
		t += s.Area()
	}
	return round(round(t))
}

func round(x float64) float64 { return x }
`,
		"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestTotal(t *testing.T) { Total(nil) }\n",
		"b/b.go":      "// Package b multiplies.\npackage b\n\n// Mul multiplies.\nfunc Mul(x, y float64) float64 { return x * y }\n",
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := tree(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := `package example.com/m/a
type Shape interface{ Area() float64 }
	// Shape has an area.
type Sq struct{ S float64 }
	// Sq is a square.
func (q Sq) Area() float64
	// Area is the square's area.
	calls: b.Mul
func Total(ss []Shape) float64
	// Total adds the areas of shapes.
	calls: Shape.Area, round
func round(x float64) float64
package example.com/m/b
func Mul(x, y float64) float64
	// Mul multiplies.
`
	if got != want {
		t.Errorf("tree =\n%s\nwant\n%s", got, want)
	}
}
