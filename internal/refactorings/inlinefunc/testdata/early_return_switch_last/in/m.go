package m

func Use(k string) int {
	n := size(k)
	return n * 2
}

func size(k string) int {
	switch k {
	case "s":
		return 1
	default:
		return 3
	}
}
