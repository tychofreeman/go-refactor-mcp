package metrics

import (
	"fmt"
	"testing"
)

const twins = `package p

func A(xs []int) int {
	total := 0
	for _, x := range xs {
		// doubled

		total += x * 2
	}
	return total
}

func B(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x * 2
	}
	return total
}

func C(xs []int) int {
	total := 1
	for _, x := range xs {
		total += x * 2
	}
	return total
}
`

func TestDuplicated(t *testing.T) {
	files := map[string][]byte{"a.go": []byte(twins), "a_test.go": []byte(twins)}
	if got := Duplicated(files); got != 8 {
		t.Errorf("Duplicated = %d, want 8: the last four significant lines of A and B", got)
	}
	if got := Duplicated(map[string][]byte{"b.go": []byte(b)}); got != 0 {
		t.Errorf("Duplicated without duplicates = %d, want 0", got)
	}
}

func TestChanged(t *testing.T) {
	before := map[string][]byte{"a.go": []byte(a), "b.go": []byte(b)}
	after := map[string][]byte{
		"a.go":      []byte(a + "\nfunc New() int { return 1 }\n"),
		"b.go":      []byte("package p\n\nfunc helper() bool { return false }\n"),
		"b_test.go": []byte(aTest),
	}
	got, err := Changed(before, after)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range got {
		names = append(names, f.File+":"+f.Name)
	}
	if want := "[a.go:New b.go:helper]"; fmt.Sprint(names) != want {
		t.Errorf("Changed = %v, want %s", names, want)
	}
}
