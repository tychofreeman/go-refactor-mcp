package m

type (
	id int

	// mover moves.
	mover struct {
		path string
		n    int
	}
)

func (m mover) size() int { return len(m.path) + m.n }
