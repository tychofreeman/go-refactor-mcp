package m

func days(kind string, n int) int {
	switch kind {
	case "book":
		return 28
	case "dvd":
		return 7
	}
	return n
}
