package m

type mover struct {
	decl string
	n    int
}

func size(m *mover, extra int) int {
	return len(m.decl) + m.n + extra
}

func Use(mv *mover, v mover) int {
	return size(mv, 1) + size(&v, 2)
}
