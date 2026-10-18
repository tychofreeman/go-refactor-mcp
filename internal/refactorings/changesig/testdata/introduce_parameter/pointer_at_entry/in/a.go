package m

type mover struct {
	decl string
	n    int
}

func (m *mover) check() int {
	switch m.decl {
	case "a":
		m.n++
		return 1
	}
	return 0
}

func Use(mv *mover, v mover) int {
	return mv.check() + v.check()
}
