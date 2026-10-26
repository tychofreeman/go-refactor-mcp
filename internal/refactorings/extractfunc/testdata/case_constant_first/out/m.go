package m

const book = "novel"

func days(kind string, n int) int {
	switch kind {
	case "book":
		return 21
	case book:
		return bookDays(n)
	}
	return 0
}

func bookDays(n int) int {
	return n
}
