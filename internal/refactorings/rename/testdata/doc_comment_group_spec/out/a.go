package m

// mover moves.
type mover struct {
	// path is absolute.
	path string
	n    int // n counts
}

var (
	// ceiling caps n.
	ceiling = 3
	other   = 4
)

func (m mover) size() int { return len(m.path) + m.n + ceiling + other }
