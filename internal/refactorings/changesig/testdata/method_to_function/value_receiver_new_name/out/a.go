package m

type point struct{ x, y int }

func pointSum(p point) int { return p.x + p.y }

func Use(q *point, r point) int { return pointSum(*q) + pointSum(r) }
