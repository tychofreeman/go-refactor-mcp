package m

import "strings"

func f(sep string, parts ...string) string {
	return strings.Join(parts, sep)
}

func Use() string { return f(",", "a", "b") + f(",") }
