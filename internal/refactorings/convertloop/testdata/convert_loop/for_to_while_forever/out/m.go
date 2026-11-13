package m

func first(a []int) int {
	i := 0
	for {
		if a[i] > 0 {
			return i
		}
		i++
	}
}
