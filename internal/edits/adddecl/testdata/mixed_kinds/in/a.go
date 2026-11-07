package m

// point is a point.
type point struct{ x, y int }

func (p point) X() int { return p.x }

func origin() point { return point{} }
