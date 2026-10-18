package m

type mover struct{ n int }

func size(k int, m *mover) int { return m.n + k }
