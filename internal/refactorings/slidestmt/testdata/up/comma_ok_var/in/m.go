package m

func F(i, j any) int {
	var v, ok = i.(int)
	var w, ok2 = j.(int)
	if !ok || !ok2 {
		return 0
	}
	return v + w
}
