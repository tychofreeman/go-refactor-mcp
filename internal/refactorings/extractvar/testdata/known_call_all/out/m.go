package m

import "strings"

func f(s string) bool {
	lower := strings.ToLower(s)
	if lower == "a" {
		return true
	}
	return lower == "b"
}
