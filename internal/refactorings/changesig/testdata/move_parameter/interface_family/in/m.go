package m

type Shape interface {
	Area(scale int, unit string) int
}

type Square struct{ side int }

func (s Square) Area(scale int, unit string) int { return s.side * s.side * scale }

func total(s Shape) int { return s.Area(1, "m") }
