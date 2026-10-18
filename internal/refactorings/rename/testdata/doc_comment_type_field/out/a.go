package m

// mover moves.
type mover struct {
	// file is absolute.
	file string
	n    int // n counts
}

var (
	// limit caps n.
	limit = 3
	other = 4
)

func (m mover) size() int { return len(m.file) + m.n + limit + other }
