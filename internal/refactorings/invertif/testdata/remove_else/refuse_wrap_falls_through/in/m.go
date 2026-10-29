package m

func sign(n int) string {
	s := "pos"
	if n < 0 {
		s = "neg"
	}
	return s
}
