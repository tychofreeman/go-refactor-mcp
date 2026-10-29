package m

// Soil is a bed's soil.
type Soil struct{ ph int }

func (s *Bed) acid() bool { return s.Soil.ph < 6 }

// Bed is a garden bed.
type Bed struct {
	Soil
	ph int
}

// Ready says whether the bed is ready.
func (b *Bed) Ready() bool { return !b.acid() }
