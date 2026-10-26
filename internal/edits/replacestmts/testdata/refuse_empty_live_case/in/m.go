package m

// Plant is something in a bed.
type Plant struct {
	Name    string
	Species any
}

// Water is how many litres p needs.
func Water(p Plant) int {
	switch p.Species {
	case "fern":
		return 3
	case "cactus":
		return 1
	}
	return 0
}

// Parse makes a plant of a name and a species.
func Parse(name, species string) Plant {
	return Plant{Name: name, Species: species}
}

// Fern makes p a fern.
func Fern(p *Plant) { p.Species = "fern" }

// Set gives p the species v.
func Set(p *Plant, v any) { p.Species = v }

// Cactus makes p a cactus.
func Cactus(p *Plant) { p.Species = "cactus" }
