package m

type mover struct{ n int }

func size(k int, m *mover) int { return m.n + k }

func use(m *mover) int { return size(2, m) }
