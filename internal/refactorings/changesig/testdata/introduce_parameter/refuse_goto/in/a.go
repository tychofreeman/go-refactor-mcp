package m

type mover struct {
	decl string
	n    int
}

func (m *mover) size() int {
again:
	k := len(m.decl)
	if m.n < 3 {
		m.n++
		goto again
	}
	return k
}

func Use(mv *mover) int { return mv.size() }
