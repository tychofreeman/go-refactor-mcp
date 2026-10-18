package m

type mover struct {
	decl string
	n    int
}

func (m *mover) check(decl string) int {
	switch decl {
	case "a":
		m.n++
		return 1
	}
	return 0
}

func Use(mv *mover, v mover) int {
	return mv.check(mv.decl) + v.check(v.decl)
}
