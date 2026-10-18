package m

import "strings"

func f(s string) bool {
	if strings.ToLower(s) == "a" {
		return true
	}
	return strings.ToLower(s) == "b"
}
