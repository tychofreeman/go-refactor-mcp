package m

type grower interface{ grow(by int) }

type box struct{ n int }

func (b *box) grow(by int) { b.n += by }

var _ grower = (*box)(nil)
