package m

// Bed is a garden bed.
type Bed struct{ depth int }

func (b *Bed) reset() { b.depth = 0 }

func (b *Bed) water(depth int) int {
	b.reset()
	return depth * 2
}

// Plant waters the bed.
func (b *Bed) Plant() int { return b.water(b.depth) }
