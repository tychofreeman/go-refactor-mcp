package m

// Plant is something in a bed.
type Plant struct{ Title string }

// Species is a kind of plant.
type Species interface {
	Label(it Plant) string
}

// Fern is a species.
type Fern struct{}

// Moss is a species.
type Moss struct{}

func (f Fern) Label(it Plant) string { return it.Title }

func (m Moss) Label(it Plant) string {
	it.Title += " (moss)"
	return it.Title
}

// Label shows it as s.
func Label(s Species, it Plant) string { return s.Label(it) }
