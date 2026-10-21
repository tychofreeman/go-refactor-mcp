package m

type Shape interface {
	Area(scale int64) int64
}

type Square struct{ side int64 }

func (s Square) Area(scale int64) int64 { return s.side * int64(int32(scale)) }

func total(s Shape, k int32) int64 { return s.Area(int64(k)) + Square{2}.Area(3) }
