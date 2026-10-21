package m

type Shape interface {
	Area(unit string, scale int) int
}

type Square struct{ side int }

func (s Square) Area(unit string, scale int) int { return s.side * s.side * scale }

func total(s Shape) int { return s.Area("m", 1) }
