package m

func check(a, b int) bool {
	var ok bool
	greater := a > b
	if ok = greater; ok {
		return true
	}
	return false
}
