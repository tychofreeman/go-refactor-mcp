package m

import "strings"

func plan(name string) (int, string) {
	n := len(name)
	println(n)
	if done, v1, v2 := step(n, name); done {
		return v1, v2
	}
	return n, ""
}

func step(n int, name string) (bool, int, string) {
	if n > 3 {
		return true, 0, strings.TrimSpace(name)
	}
	println("short")
	return false, 0, ""
}
