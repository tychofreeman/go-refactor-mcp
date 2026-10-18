package m

type mover struct {
	decl string
	n    int
}

func (m *mover) size(extra int) int {
	return len(m.decl) + m.n + extra
}

func Use(mv *mover, v mover) int {
	return mv.size(1) + v.size(2)
}
