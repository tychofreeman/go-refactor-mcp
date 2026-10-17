package m

func between(lo, hi int, v int) bool {
	return lo <= v && v <= hi
}

func Use() bool { return between(1, 9, 5) }
