package m

func kind(s string) int {
	n := 0
	switch s {
	case "a":
		n = 1
		fallthrough
	case "c":
		n++
	}
	return n
}
