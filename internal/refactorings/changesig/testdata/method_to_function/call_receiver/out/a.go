package m

type mover struct{ n int }

func newMover() *mover { return &mover{} }

func size(m *mover, k int) int { return m.n + k }

func Use(ms []mover) int { return size(newMover(), 1) + size(&ms[0], 2) }
