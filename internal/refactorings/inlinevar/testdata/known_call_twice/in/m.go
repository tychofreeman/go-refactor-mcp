package m

import "strings"

func f(s string) string {
	u := strings.ToUpper(s)
	return u + u
}
