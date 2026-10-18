package m

func F(i, j any) int {
	var w, ok2 = j.(int)
	var v, ok = i.(int)
	if !ok || !ok2 {
		return 0
	}
	return v + w
}
