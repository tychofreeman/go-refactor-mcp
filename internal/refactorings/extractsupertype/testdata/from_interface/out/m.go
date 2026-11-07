package m

// Size has the methods Depth and Width of Bed.
type Size interface {
	// Depth is how deep the soil is.
	Depth() int
	Width() int
}

// Bed is a bed of plants.
type Bed interface {
	Size
	Name() string
}
