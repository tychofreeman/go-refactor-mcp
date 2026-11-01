package m

// Bed is a garden bed.
type Bed struct {
	Name string
	Rows int
}

func Label(b Bed) string { return b.Name }
