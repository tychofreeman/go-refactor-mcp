package m

type Shape interface {
	Area(scale int32) int64
}

type Square struct{ side int64 }

func (s Square) Area(scale int32) int64 { return s.side * int64(scale) }

func total(s Shape, k int32) int64 { return s.Area(k) + Square{2}.Area(3) }
