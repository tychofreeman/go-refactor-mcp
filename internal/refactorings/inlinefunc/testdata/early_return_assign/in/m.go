package m

func Use(n int) string {
	s := sign(n)
	return s + "!"
}

func sign(n int) string {
	if n < 0 {
		return "neg"
	}
	if n == 0 {
		return "zero"
	}
	return "pos"
}
