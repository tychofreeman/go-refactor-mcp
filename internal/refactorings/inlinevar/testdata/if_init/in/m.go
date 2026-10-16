package m

func Big(xs []int) bool {
	if n := len(xs); n > 3 {
		return true
	}
	return false
}
