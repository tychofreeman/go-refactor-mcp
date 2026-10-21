package m

type Mover struct{ n int }

func (m *Mover) Size() int { return m.n }
