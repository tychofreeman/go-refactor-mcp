package m

func days(kind string, n int) int {
	switch kind {
	case "book":
		return 21
	case "dvd", "cd", "tape":
		// a week
		return 7 * n
	}
	return n
}
