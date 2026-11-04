package m

// Soil is what a bed is filled with.
type Soil struct {
	// depth is in centimetres.
	depth int
	loam  bool
}

// till turns the soil over.
func (s *Soil) till() { s.loam = true }

// deep reports whether s is deep enough.
func (s Soil) deep() bool { return s.depth > 2 }

// Bed is a bed of plants.
type Bed struct {
	Soil
	name string
}

func newBed(n string) Bed { return Bed{Soil{3, true}, n} }
