package m

func days(kind string, n int) int {
	switch kind {
	case "book":
		return 21
	case "cd":
		// a week
		return 7 * n
	case "tape":
		// a week
		return 7 * n
	case "dvd":
		// a week
		return dvdDays(n)
	}
	return n
}

func dvdDays(n int) int {
	return 7 * n
}
