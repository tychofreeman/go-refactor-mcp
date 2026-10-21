package m

type Shape interface {
	Area(scale int, unused bool) int
}

type Square struct{ side int }

func (s Square) Area(scale int, unused bool) int { return s.side * s.side * scale }

type Rect struct{ w, h int }

func (r *Rect) Area(scale int, unused bool) int {
	if unused {
		return 0
	}
	return r.w * r.h * scale
}

func total(s Shape) int { return s.Area(1, true) }
