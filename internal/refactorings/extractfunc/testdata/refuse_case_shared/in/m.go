package m

func days(kind string, n int) int {
	switch kind {
	case "book":
		return 21
	case "dvd", "cd":
		return 7
	}
	return n
}
