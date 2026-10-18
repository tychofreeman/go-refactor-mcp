package m

type mover struct{ n int }

func (m *mover) inc() { m.n++ }

type outer struct {
	mover
	name string
}

type ptrOuter struct {
	*mover
}

func Use(o outer, p *outer, q ptrOuter) {
	o.inc()
	p.inc()
	q.inc()
}
