package m

func first(a []int) int {
	for i := 0; ; i++ {
		if a[i] > 0 {
			return i
		}
	}
}
