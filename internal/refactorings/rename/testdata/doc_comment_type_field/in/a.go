package m

// mover moves.
type mover struct {
	// path is absolute.
	path string
	n    int // n counts
}

var (
	// limit caps n.
	limit = 3
	other = 4
)

func (m mover) size() int { return len(m.path) + m.n + limit + other }
