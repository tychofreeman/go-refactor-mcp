package m

func Grow(out []int, v int) []int {
	next := append(out, v*2)
	out = next
	return out
}
