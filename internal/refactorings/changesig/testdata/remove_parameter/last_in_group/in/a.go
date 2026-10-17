package m

func area(w, h, unused int) int {
	return w * h
}

func Use() int {
	return area(2, 3, 4) + area(5, 6, 7)
}
