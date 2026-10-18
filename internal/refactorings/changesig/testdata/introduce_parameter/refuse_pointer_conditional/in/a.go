package m

type mover struct {
	decl string
	n    int
}

func (m *mover) size() int {
	if m.n > 0 {
		return len(m.decl)
	}
	return 0
}

func Use(mv *mover) int { return mv.size() }
