package m

type (
	id int

	source struct {
		path string
	}

	// mover moves.
	mover struct {
		source
		n int
	}
)

func (m mover) size() int { return len(m.path) + m.n }
