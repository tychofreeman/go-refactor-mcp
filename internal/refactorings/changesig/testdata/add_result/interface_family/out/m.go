package m

type Shape interface {
	Area() (int, error)
}

type Square struct{ side int }

func (s Square) Area() (int, error) { return s.side * s.side, nil }

func total(s Shape) int {
	n, _ := s.Area()
	return n
}
