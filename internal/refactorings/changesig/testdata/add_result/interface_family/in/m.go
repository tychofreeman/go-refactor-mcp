package m

type Shape interface {
	Area() int
}

type Square struct{ side int }

func (s Square) Area() int { return s.side * s.side }

func total(s Shape) int {
	n := s.Area()
	return n
}
