package m

func span(lo int, label string, hi int) int {
	return hi - lo
}

var X = span(1, "x", 5)
