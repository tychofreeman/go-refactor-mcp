package m

func kind(s string) int {
	n := 0
	switch s {
	case "x":
		n = 9
	case "a":
		n = 1
		fallthrough
	default:
		n++
	}
	return n
}
