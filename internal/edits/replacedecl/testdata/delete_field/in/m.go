package m

// Bed is a garden bed.
type Bed struct {
	Name string
	// Soil is what grows in it.
	Soil string // loam or clay
	Rows int
}

func Label(b Bed) string { return b.Name }
