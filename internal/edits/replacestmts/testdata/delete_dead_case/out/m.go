package m

// Fern is a species.
type Fern struct{}

// Plant is something in a bed.
type Plant struct {
	Name    string
	Species any
}

// Water is how many litres p needs.
func Water(p Plant) int {
	switch p.Species {
	case "fern":
	case "cactus":
		return 1
	}
	return 0
}

// Rose is the bed's first plant.
var Rose = Plant{Name: "rose", Species: Fern{}}

// Cactus makes p a cactus.
func Cactus(p *Plant) { p.Species = "cactus" }
