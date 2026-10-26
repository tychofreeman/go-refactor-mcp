package garden

// Species is a kind of plant.
type Species interface{ Water() int }

// Fern is a species.
type Fern struct{}

func (Fern) Water() int { return 3 }

// Cactus is a species.
type Cactus struct{}

func (Cactus) Water() int { return 1 }

// Plant is something in a bed.
type Plant struct {
	Name    string
	Species Species
}

// Water is how much p needs.
func Water(p Plant) int {
	if s, ok := p.Species.(Species); ok {
		return s.Water()
	}
	switch p.Species {
	}
	return 0
}

// Bed is the first bed.
var Bed = []Plant{{Name: "a", Species: Fern{}}, {"b", Cactus{}}}

// Dry reports whether p needs little water.
func Dry(p Plant) bool { return p.Species == (Cactus{}) }
