package m

type Point struct{ x int }

func (p Point) Norm() int { return p.x }

func twice(p Point) int { return 2 * p.Norm() }
