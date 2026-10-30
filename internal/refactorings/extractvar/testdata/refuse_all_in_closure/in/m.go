package m

type Bed struct{ depth int }

func water(b *Bed) func() int {
	a := b.depth
	return func() int { return a + b.depth }
}
