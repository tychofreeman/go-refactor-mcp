package m

type T struct{ xs []int }

func f(n int) int {
	d := n * 2
	return d + 1
}

func run(p *T, n int) {
	p.xs[0] = f(n)
}
