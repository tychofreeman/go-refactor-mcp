package m

func water(height int) int {
	if height > 20 {
		return 5
	} else if height > 10 {
		return 3
	}
	return 1
}
