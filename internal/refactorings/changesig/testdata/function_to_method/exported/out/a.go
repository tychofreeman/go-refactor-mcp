package m

type mover struct{ n int }

func (m *mover) Size() int { return m.n }
