package m

type grower interface{ grow(by, limit int) }

type box struct{ n int }

func (b *box) grow(by, limit int) { b.n += by }

var _ grower = (*box)(nil)
