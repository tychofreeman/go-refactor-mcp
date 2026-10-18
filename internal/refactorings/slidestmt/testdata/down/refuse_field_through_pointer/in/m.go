package m

type pt struct{ x, y int }

func F(p *pt) {
	q := p
	p.x = 1
	_ = q.x
}
