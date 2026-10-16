package m

func TooMany(xs []int) bool {
	limit := 10
	if len(xs) > limit {
		return true
	}
	return false
}
