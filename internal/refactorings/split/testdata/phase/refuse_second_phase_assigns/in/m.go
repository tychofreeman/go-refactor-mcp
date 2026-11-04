package m

// Bed is a bed of plants.
type Bed struct {
	Area   int
	Plants int
}

// Cost prices watering b for days at rate.
func Cost(b Bed, days, rate int) int {
	litres := b.Area * 2
	extra := b.Plants / 3
	litres = litres + extra
	return litres * days * rate
}
