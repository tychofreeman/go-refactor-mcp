package m

type mover struct {
	decl string
	n    int
}

func (m *mover) size(decl string) int {
	k := m.n
	return k + len(decl)
}

func Use(mv *mover) int { return mv.size(mv.decl) }
