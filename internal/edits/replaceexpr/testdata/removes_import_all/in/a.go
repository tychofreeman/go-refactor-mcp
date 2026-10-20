package m

import "strings"

func f(s string) string {
	if s == "" {
		return strings.ToUpper(s)
	}
	return strings.ToUpper(s) + "!"
}
