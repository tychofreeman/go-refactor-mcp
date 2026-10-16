package m

func Check(name string, max int, strict bool) bool {
	tooLong := len(name) > max
	if tooLong && strict {
		return false
	}
	return true
}
