package m

// Bed is a garden bed.
type Bed struct{ depth int }

func (b *Bed) water(depth int) int { return depth * 2 }

// Plant waters the bed.
func (b *Bed) Plant() int { return b.water(b.depth) }

// Morning waters x.
func Morning(x *Bed) int { return x.water(x.depth) + 1 }
