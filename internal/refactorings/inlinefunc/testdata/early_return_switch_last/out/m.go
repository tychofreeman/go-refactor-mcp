package m

func Use(k string) int {
	var n int
	switch k {
	case "s":
		n = 1
	default:
		n = 3
	}
	return n * 2
}
