package m

func between(lo int, label string, hi int, v int) bool {
	return lo <= v && v <= hi
}

func Use() bool { return between(1, "x", 9, 5) }
