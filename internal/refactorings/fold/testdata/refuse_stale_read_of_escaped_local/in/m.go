package m

func note(q *int, n int) {
	*q = 2
	println(n)
}

func run() {
	x := 1
	p := &x
	*p = 2
	println(x)
}
