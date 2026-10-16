package m

func lookup() int { return 4 }

func F() bool {
	if v := lookup(); v > 3 {
		return true
	}
	return false
}
