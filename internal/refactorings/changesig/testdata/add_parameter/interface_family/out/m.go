package m

type Shape interface {
	Area(int, int) int
}

type Square struct{ side int }

func (s Square) Area(scale, offset int) int { return s.side * s.side * scale }

type Rect struct{ w, h int }

func (r *Rect) Area(scale, offset int) int { return r.w * r.h * scale }

func total(shapes []Shape) int {
	n := 0
	for _, s := range shapes {
		n += s.Area(1, 0)
	}
	return n
}

func one() int { return Square{2}.Area(3, 0) }
