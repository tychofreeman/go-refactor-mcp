package m

// Soil is a bed's soil.
type Soil struct {
	ph int
	// depth is in centimetres.
	depth int
}

// Bed is a garden bed.
type Bed struct {
	Soil
	name string
}

func newBed() Bed { return Bed{name: "a", Soil: Soil{ph: 7, depth: 3}} }

func other() Bed { return Bed{Soil: Soil{depth: 2}} }

// Deep says whether b is deep.
func (b Bed) Deep() bool { return b.depth > 2 }
