package m

// Soil is a bed's soil.
type Soil struct{ ph int }

func (s *Soil) acid() bool { return s.ph < 6 }

func (s *Soil) lime() {
	if s.acid() {
		s.ph++
	}
}

// Bed is a garden bed.
type Bed struct {
	Soil
	name string
}

// Ready says whether the bed is ready.
func (b *Bed) Ready() bool { return !b.acid() && !b.Soil.acid() }
