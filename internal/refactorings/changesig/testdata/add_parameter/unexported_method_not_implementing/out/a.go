package m

type grower interface{ grow(by int64) }

type box struct{ n int }

func (b *box) grow(by, limit int) { b.n += by }

func Use(b *box) { b.grow(2, 0) }
