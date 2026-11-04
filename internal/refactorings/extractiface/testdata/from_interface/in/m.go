package m

// Bed is a bed of plants.
type Bed interface {
	// Depth is how deep the soil is.
	Depth() int
	Width() int
	Name() string
}
