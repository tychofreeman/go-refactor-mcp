package m

func total(a, b, c, d, e, f int) int {
	s := sum(a, b, c, d, e, f)
	return s * 2
}

func sum(a int, b int, c int, d int, e int, f int) int {
	s := a + b
	s += c + d
	s += e + f
	return s
}
