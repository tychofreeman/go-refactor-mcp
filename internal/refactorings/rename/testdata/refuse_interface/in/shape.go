package m

type Shape interface{ Area() float64 }

type Square struct{ side float64 }

func (q Square) Area() float64 { return q.side * q.side }

func Total(shapes []Shape) float64 {
	t := 0.0
	for _, s := range shapes {
		t += s.Area()
	}
	return t
}

var _ = Total([]Shape{Square{2}})
