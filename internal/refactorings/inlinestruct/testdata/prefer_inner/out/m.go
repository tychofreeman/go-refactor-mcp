package m

// till turns the soil over.
func (s *Bed) till() { s.loam = true }

// deep reports whether s is deep enough.
func (s Bed) deep() bool { return s.depth > 2 }

// Bed is a bed of plants.
type Bed struct {
	// depth is in centimetres.
	depth int
	loam  bool
	name  string
}

func newBed(n string) Bed { return Bed{depth: 3, name: n} }

func ready(b *Bed) bool {
	b.till()
	return b.deep() && b.depth > 1 && b.loam
}
