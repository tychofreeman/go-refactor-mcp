package m

type Bed struct{ depth int }

func grow(b *Bed) { b.depth++ }

func water(b *Bed, n int) int {
	d := b.depth
	total := d
	for i := 0; i < n; i++ {
		total += d
		grow(b)
	}
	return total
}
