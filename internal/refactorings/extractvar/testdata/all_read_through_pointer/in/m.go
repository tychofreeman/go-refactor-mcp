package m

type Bed struct{ depth int }

func water(b *Bed) int {
	return b.depth*2 + b.depth
}
