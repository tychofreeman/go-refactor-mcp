package m

type Spanner interface{ Span(lo, hi int) int }

type T struct{}

func (T) Span(hi, lo int) int { return hi - lo }

var _ Spanner = T{}

var X = T{}.Span(5, 1)
