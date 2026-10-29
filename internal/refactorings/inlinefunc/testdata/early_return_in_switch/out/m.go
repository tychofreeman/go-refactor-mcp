package m

func Use(k string) int {
	n := 3
	switch k {
	case "s":
		n = 1
	case "m":
		n = 2
	}
	return n * 2
}
