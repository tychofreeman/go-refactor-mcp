package m

import "strings"

var counter int

func f(s string) int {
	return len(strings.FieldsFunc(s, func(rune) bool { counter++; return false }))
}

func Use() int { return f("abc") }
