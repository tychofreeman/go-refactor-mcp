package m

// Soil is a bed's soil.
type Soil struct{ ph int }

func (s *Soil) acid() bool { return s.ph < 6 }

// Bed is a garden bed.
type Bed struct {
	soil Soil
}

// Ready says whether the bed is ready.
func (b *Bed) Ready() bool { return !b.soil.acid() }
