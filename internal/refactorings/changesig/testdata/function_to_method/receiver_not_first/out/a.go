package m

type mover struct{ n int }

func (m *mover) size(k int) int { return m.n + k }

func use(m *mover) int { return m.size(2) }
