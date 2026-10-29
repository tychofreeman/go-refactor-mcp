package m

// Bed is a garden bed.
type Bed struct{ depth int }

func (b *Bed) soak(depth, times int) int {
	d := depth
	if times == 0 {
		return d
	}
	return d + b.soak(depth, times-1)
}

// Soak soaks the bed.
func (b *Bed) Soak() int { return b.soak(b.depth, 3) }
