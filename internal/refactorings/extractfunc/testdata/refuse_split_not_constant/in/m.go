package m

func short() string { return "dvd" }

func days(kind string, n int) int {
	switch kind {
	case "book", short():
		return 7 * n
	}
	return n
}
