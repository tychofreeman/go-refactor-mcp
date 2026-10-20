package m

import "strings"

var counter int

func f(s string) int {
	n := 0
	for i := 0; i < 3; i++ { n += len(strings.FieldsFunc(s, func(rune) bool { counter++; return false })) }
	return n
}

func Use() int { return f("abc") }
