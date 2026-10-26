package m

type T struct{ xs []int }

func f(p *T, n int) int {
	p.xs = make([]int, 1)
	return n + 1
}

func run(p *T, n int) {
	p.xs[0] = f(p, n)
}
