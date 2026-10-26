package m

func water(height int) int {
	if height > 10 {
		return 3
	} else if height > 5 {
		return 2
	} else if height > 0 {
		return 1
	}
	return 0
}
