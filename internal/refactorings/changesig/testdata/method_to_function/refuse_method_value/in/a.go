package m

type mover struct{ n int }

func (m *mover) size() int { return m.n }

func Use(m *mover) func() int { return m.size }
