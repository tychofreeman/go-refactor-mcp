package m

func F(i any) int {
	n := 3
	v, ok := i.(int)
	if !ok {
		return n
	}
	return v + n
}
