package m

func F(a, b int) int {
	if a > 0 {
		goto done
	}
	a = a + b*2
done:
	return a
}
