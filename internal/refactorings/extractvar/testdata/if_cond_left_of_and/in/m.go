package m

func Check(name string, max int, strict bool) bool {
	if len(name) > max && strict {
		return false
	}
	return true
}
