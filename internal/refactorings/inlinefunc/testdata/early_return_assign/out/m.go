package m

func Use(n int) string {
	var s string
	if n < 0 {
		s = "neg"
	} else {
		if n == 0 {
			s = "zero"
		} else {
			s = "pos"
		}
	}
	return s + "!"
}
