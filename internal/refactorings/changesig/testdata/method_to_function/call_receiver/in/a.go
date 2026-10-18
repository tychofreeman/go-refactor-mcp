package m

type mover struct{ n int }

func newMover() *mover { return &mover{} }

func (m *mover) size(k int) int { return m.n + k }

func Use(ms []mover) int { return newMover().size(1) + ms[0].size(2) }
