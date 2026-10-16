package m

func Grow(out []int, v int) []int {
	out = append(out, v*2)
	return out
}
