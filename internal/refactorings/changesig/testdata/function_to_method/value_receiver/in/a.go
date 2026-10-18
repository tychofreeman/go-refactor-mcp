package m

type point struct{ x, y int }

func pointSum(p point) int { return p.x + p.y }

func Use(q *point, r point, ps []point) int { return pointSum(*q) + pointSum(r) + pointSum(ps[0]) }
