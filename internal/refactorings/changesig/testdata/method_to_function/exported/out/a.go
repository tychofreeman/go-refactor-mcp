package m

type Point struct{ x int }

func Norm(p Point) int { return p.x }

func twice(p Point) int { return 2 * Norm(p) }
