package m

type T struct{ xs []int }

func run(p *T, n int) {
	d := n * 2
	p.xs[0] = d + 1
}
