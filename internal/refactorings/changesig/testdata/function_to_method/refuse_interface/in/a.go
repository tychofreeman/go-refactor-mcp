package m

type sizer interface{ size() int }

type mover struct{ n int }

func size(m *mover) int { return m.n }

func Use(s sizer) int { return s.size() }
