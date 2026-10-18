package m

type mover struct{ n int }

func inc(m *mover) { m.n++ }

type outer struct {
	mover
	name string
}

type ptrOuter struct {
	*mover
}

func Use(o outer, p *outer, q ptrOuter) {
	inc(&o.mover)
	inc(&p.mover)
	inc(q.mover)
}
