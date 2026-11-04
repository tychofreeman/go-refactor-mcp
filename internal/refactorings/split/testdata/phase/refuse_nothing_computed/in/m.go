package m

// Bed is a bed of plants.
type Bed struct {
	Area   int
	Plants int
}

// Cost prices watering b for days at rate.
func Cost(b Bed, days, rate int) int {
	b.Area *= 2
	return b.Plants * days * rate
}
