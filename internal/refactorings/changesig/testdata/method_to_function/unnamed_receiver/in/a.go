package m

type mover struct{ n int }

func (*mover) zero() int { return 0 }

func (m *mover) twice() int { return m.zero() * 2 }
