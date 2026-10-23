package m

import "strings"

func F(k int) string {
	switch k {
	case 1:
		return strings.ToUpper("one")
	// two is rare
	case 2:
		return "two"
	default:
		return "other"
	}
}
