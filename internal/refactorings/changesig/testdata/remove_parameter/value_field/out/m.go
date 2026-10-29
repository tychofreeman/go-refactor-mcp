package m

// Bed is a garden bed.
type Bed struct{ depth int }

func (b *Bed) water() int { return b.depth * 2 }

// Plant waters the bed.
func (b *Bed) Plant() int { return b.water() }

// Morning waters x.
func Morning(x *Bed) int { return x.water() + 1 }
