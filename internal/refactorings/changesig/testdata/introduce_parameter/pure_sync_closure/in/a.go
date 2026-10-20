package m

import "strings"

func f(s string) int {
	return len(strings.FieldsFunc(s, func(rune) bool { return false }))
}

func Use() int { return f("a b") }
