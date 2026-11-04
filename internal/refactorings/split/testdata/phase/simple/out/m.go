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
	usage := Usage{litres: litres, extra: extra}
	return price(usage, days, rate)
}

// Usage holds what Cost computes first, for the rest of it.
type Usage struct {
	litres int
	extra  int
}

func price(usage Usage, days int, rate int) int {
	total := (usage.litres + usage.extra) * days
	return total * rate
}
