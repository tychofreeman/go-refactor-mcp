package m

// till turns the soil over.
func (s *Bed) till() { s.loam = true }

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
	return b.depth > 1 && b.loam
}

// deep reports whether b is deep enough for its plants.
func (b Bed) deep() bool { return b.depth > 5 }
