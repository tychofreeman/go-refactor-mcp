package m

type Shape interface {
	Area(scale int, unused bool) int
}

type Square struct{ side int }

func (s Square) Area(scale int, unused bool) int { return s.side * s.side * scale }

type Rect struct{ w, h int }

func (r *Rect) Area(scale int, _ bool) int { return r.w * r.h * scale }

func total(shapes []Shape) int {
	n := 0
	for _, s := range shapes {
		n += s.Area(1, true)
	}
	return n
}

func one() int { return Square{2}.Area(3, false) }
