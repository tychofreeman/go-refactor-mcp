package m

import "strings"

func f(s string) int {
	return len(strings.FieldsFunc(s, func(r rune) bool { sp := r == 32; return sp }))
}

func Use() int { return f("a b") }
