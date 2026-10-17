package m

func area(w, h int) int {
	return w * h
}

func Use() int {
	return area(2, 3) + area(5, 6)
}
