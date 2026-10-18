package m

type mover struct {
	decl string
	n    int
}

func (m *mover) size(xs []int) int {
	k := xs[0]
	return k + len(m.decl)
}

func Use(mv *mover) int { return mv.size(nil) }
