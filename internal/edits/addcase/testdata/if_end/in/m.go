package m

func water(height int) int {
	if height > 10 {
		return 3
	} else if height > 5 {
		return 2
	} else {
		return 1
	}
}
