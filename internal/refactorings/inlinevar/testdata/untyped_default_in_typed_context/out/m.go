package m

func TooMany(xs []int) bool {
	if len(xs) > 10 {
		return true
	}
	return false
}
