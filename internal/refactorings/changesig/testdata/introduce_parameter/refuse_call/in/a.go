package m

import "strings"

func f(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s)
}

func Use() string { return f("a") }
