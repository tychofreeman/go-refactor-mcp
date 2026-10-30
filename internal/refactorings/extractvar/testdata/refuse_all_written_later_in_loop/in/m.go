package m

type Bed struct{ depth int }

func grow(b *Bed) { b.depth++ }

func water(b *Bed, n int) int {
	total := b.depth
	for i := 0; i < n; i++ {
		total += b.depth
		grow(b)
	}
	return total
}
