package m

import "strings"

func Count(s string) int {
	n := 0
	words := strings.Fields(s)
	for _, w := range words {
		n += len(w)
	}
	return n
}
