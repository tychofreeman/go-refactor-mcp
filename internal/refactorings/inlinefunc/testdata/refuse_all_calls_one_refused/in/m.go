package m

func a() int {
	return pick(1)
}

func b() {
	defer pick(2)
}

func pick(n int) int {
	if n > 1 {
		return n
	}
	return 0
}
