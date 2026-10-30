package m

type Bed struct{ depth int }

func water(b *Bed) int {
	d := b.depth
	return d*2 + d
}
