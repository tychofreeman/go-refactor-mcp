package m

// point is a point.
type point struct{ x, y int }

func (p point) X() int { return p.x }

// Norm is x plus y.
func (p point) Norm() int { return p.x + p.y }

func origin() point { return point{} }

// size is a count of points.
type size int

func (s size) twice() size { return 2 * s }
