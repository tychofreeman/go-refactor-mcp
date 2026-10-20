package m

type Shape interface{ Area() float64 }

type Square struct{ side float64 }

func (q Square) size() float64 { return q.side * q.side }

func Kind(v any) string {
	if _, ok := v.(Shape); ok {
		return "shape"
	}
	return "other"
}

var _ = Kind(Square{2}) + Kind(Square{1}.size())
