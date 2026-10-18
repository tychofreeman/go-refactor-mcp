package m

type base struct{ path string }

type mover struct {
	base
	path string
	n    int
}

func (m mover) p() string { return m.path }
