package m

// area is the area of a w by h rectangle.
func area(w, h int) int {
	a := w
	a *= h
	return a
}

func use() int { return area(2, 3) }
