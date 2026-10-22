package m

type Shape interface{ Area() float64 }

type Square struct{ side float64 }

func (q Square) Area() float64 { return q.side * q.side }

func Kind(v any) bool {
	_, ok := v.(Shape)
	return ok
}

var _ = Kind(1) && Square{2}.Area() > 0
