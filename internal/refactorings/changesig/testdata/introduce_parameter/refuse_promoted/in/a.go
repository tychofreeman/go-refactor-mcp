package m

type point struct{ x, y int }

func (p point) label() int { return p.x }

type named struct {
	point
	name string
}

func Use(n named) int { return n.label() }
