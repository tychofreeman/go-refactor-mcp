package m

// Bed is a bed of plants.
type Bed struct {
	Area   int
	Plants int
}

// Cost prices watering b for days at rate.
func Cost(b Bed, days, rate int) int {
	litres := b.Area * 2
	if days > 0 {
		return litres * days * rate
	}
	return 0
}
