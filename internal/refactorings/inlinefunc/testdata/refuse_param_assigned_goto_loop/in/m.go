package m

func step(n int) int {
	n++
	return n * 2
}

func caller() int {
	n, i, total := 5, 0, 0
	var m int
L:
	total += n
	m = step(n)
	i++
	if i < 3 {
		goto L
	}
	return total + m
}
