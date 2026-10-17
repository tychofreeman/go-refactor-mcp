package m

type Point struct {
	X, Y int
} // a position

const (
	Max = 10
)

var origin = Point{0, 0}

func Use() Point { return origin }
