package m

func days(kind string, n int) int {
	switch kind {
	case "book":
		return 21 * n
	}
	return n
}
