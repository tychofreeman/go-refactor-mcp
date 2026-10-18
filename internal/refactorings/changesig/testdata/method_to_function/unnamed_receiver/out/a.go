package m

type mover struct{ n int }

func zero(m *mover) int { return 0 }

func (m *mover) twice() int { return zero(m) * 2 }
