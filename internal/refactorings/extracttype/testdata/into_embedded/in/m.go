package m

// Soil is a bed's soil.
type Soil struct {
	ph int
}

// Bed is a garden bed.
type Bed struct {
	Soil
	name string
	// depth is in centimetres.
	depth int
}

func newBed() Bed { return Bed{name: "a", depth: 3, Soil: Soil{ph: 7}} }

func other() Bed { return Bed{depth: 2} }

// Deep says whether b is deep.
func (b Bed) Deep() bool { return b.depth > 2 }
