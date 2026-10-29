package m

// Soil is a bed's soil.
type Soil struct{ ph int }

func (s *Bed) acid() bool { return s.ph < 6 }

func check(s *Soil) bool { return s != nil }

func (s *Bed) ok() bool { return check(&s.Soil) }

// Bed is a garden bed.
type Bed struct {
	Soil
	name string
}

// Ready says whether the bed is ready.
func (b *Bed) Ready() bool { return !b.acid() && !b.acid() && b.ok() }
