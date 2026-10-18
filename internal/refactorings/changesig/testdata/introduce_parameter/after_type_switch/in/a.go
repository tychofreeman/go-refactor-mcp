package m

type mover struct {
	decl any
	n    int
}

func (m *mover) size() int {
	k := 0
	switch d := m.decl.(type) {
	case string:
		k = len(d)
	}
	return k + m.n
}

func Use(mv *mover) int { return mv.size() }
