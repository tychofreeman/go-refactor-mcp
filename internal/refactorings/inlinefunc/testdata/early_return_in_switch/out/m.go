package m

func Use(k string) int {
	var n int
	var returned bool
	switch k {
	case "s":
		n = 1
		returned = true
	case "m":
		n = 2
		returned = true
	}
	if !returned {
		n = 3
	}
	return n * 2
}
