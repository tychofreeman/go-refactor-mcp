package m

type mover struct {
	decl string
	n    int
}

func (m *mover) size() int {
	m.n++
	return len(m.decl)
}

func Use(mv *mover) int { return mv.size() }
