package m

import "strings"

func F() string {
	b := strings.Builder{}
	b.WriteString("x")
	return b.String()
}
