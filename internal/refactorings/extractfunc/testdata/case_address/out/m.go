package m

func days(kind string, n int) int {
	switch kind {
	case "book":
		return bookDays(n)
	case "dvd", "cd":
		return 7
	default:
		return n
	}
}

func bookDays(n int) int {
	d := 21
	return d * n
}
