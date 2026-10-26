package m

func days(kind string, n int) int {
	switch kind {
	case "book":
		return 21
	default:
		return otherDays(n)
	}
}

func otherDays(n int) int {
	return n
}
