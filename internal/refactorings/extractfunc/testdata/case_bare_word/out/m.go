package m

func days(kind string, n int) int {
	switch kind {
	case "book":
		return bookDays(n)
	}
	return n
}

func bookDays(n int) int {
	return 21 * n
}
