package m

type sizer interface{ size() int }

type mover struct{ n int }

func (m *mover) size() int { return m.n }

func Use(s sizer) int { return s.size() }
