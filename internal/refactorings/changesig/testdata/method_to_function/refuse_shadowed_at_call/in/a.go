package m

type mover struct{ n int }

func (m *mover) size() int { return m.n }

func Use(m *mover) int {
	size := 2
	return m.size() * size
}
