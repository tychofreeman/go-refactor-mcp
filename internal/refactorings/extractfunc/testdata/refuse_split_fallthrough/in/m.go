package m

func days(kind string, n int) int {
	switch kind {
	case "book":
		n++
		fallthrough
	case "dvd", "cd":
		return 7 * n
	}
	return n
}
