package m

type Bed struct{ depth int }

func grow(b *Bed) { b.depth++ }

func water(b *Bed) int {
	a := b.depth
	grow(b)
	return a + b.depth
}
