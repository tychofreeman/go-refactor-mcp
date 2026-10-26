package m

func days(kind string, n int) int {
	switch kind {
	case "book":
		d := 21
		return d * n
	case "dvd":
		return 7
	}
	return n
}
