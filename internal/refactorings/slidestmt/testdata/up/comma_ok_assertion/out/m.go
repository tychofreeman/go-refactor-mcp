package m

func F(i any) int {
	v, ok := i.(int)
	n := 3
	if !ok {
		return n
	}
	return v + n
}
