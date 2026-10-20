package m

func before(i, n int) int {
	for i++; ; {
		if i > n {
			return i
		}
		n--
	}
}

func after(i, n int) int {
	for ; ; i++ {
		if i > n {
			return i
		}
		n--
	}
}

func Use() int { return before(0, 3) + after(0, 3) }
