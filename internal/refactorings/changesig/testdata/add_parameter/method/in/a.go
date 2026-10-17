package m

type box struct{ n int }

func (b *box) grow(by int) { b.n += by }

func Use() int {
	b := &box{}
	b.grow(2)
	return b.n
}
