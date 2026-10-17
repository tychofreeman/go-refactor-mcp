package m

type prog struct{ ops []int }

func (p *prog) add(op int) int {
	i := len(p.ops)
	p.ops = append(p.ops, op)
	return i
}

func run(p *prog) {
	i := len(p.ops)
	p.ops = append(p.ops, 1)
	_ = i
	_ = p.add(2)
}
