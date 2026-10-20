package m

import "strings"

func f(s string, fields []string) int {
	return len(fields)
}

func Use() int { return f("a b", strings.FieldsFunc("a b", func(rune) bool { return false })) }
