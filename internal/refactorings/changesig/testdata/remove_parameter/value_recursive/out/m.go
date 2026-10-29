package m

// Bed is a garden bed.
type Bed struct{ depth int }

func (b *Bed) soak(times int) int {
	d := b.depth
	if times == 0 {
		return d
	}
	return d + b.soak(times-1)
}

// Soak soaks the bed.
func (b *Bed) Soak() int { return b.soak(3) }
