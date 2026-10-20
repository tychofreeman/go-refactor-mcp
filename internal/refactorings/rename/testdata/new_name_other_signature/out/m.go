package m

type Shape interface{ Area() float64 }

type Square struct{ side float64 }

func (q Square) Area() int { return int(q.side * q.side) }

func Kind(v any) bool {
	_, ok := v.(Shape)
	return ok
}

var _, _ = Kind(Square{2}), Square{1}.Area()
