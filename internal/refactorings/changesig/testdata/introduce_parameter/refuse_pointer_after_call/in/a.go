package m

type mover struct {
	decl string
	n    int
}

func (m *mover) reset() { m.decl = "" }

func (m *mover) size() int {
	m.reset()
	return len(m.decl)
}

func Use(mv *mover) int { return mv.size() }
