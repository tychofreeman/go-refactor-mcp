package m

// Shape is a figure.
type Shape interface {
	Area() float64
	Name() string
}

type Square struct{ s float64 }

func (q Square) Area() float64 { return q.s * q.s }

type Circle struct{ r float64 }

func (c *Circle) Area() float64 { return 3 * c.r * c.r }

// Other has no Area.
type Other struct{}

func (q Square) Name() string {
	panic("not implemented")
}

func (c *Circle) Name() string {
	panic("not implemented")
}
