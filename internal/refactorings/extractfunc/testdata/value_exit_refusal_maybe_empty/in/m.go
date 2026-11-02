package m

import "strings"

func plan(name string) (int, string) {
	n := len(name)
	println(n)
	if n > 3 {
		return 0, strings.TrimSpace(name)
	}
	println("short")
	return n, ""
}
