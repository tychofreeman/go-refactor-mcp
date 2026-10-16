package m

func Grade(n int) string {
	if n > 90 {
		return "A"
	} else if n*2 > 150 {
		return "B"
	}
	return "C"
}
