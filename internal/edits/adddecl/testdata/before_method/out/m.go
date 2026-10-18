package m

type edits struct{ n int }

type mover struct{ e edits }

func (m *mover) count() int { return m.e.n }

// edits returns the mover's edits.
func (m *mover) edits() edits { return m.e }

func (m *mover) other() int { return 1 }
