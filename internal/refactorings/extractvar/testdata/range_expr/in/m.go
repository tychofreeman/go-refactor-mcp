package m

import "strings"

func Count(s string) int {
	n := 0
	for _, w := range strings.Fields(s) {
		n += len(w)
	}
	return n
}
