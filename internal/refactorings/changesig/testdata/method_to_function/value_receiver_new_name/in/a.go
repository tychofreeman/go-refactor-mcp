package m

type point struct{ x, y int }

func (p point) sum() int { return p.x + p.y }

func Use(q *point, r point) int { return q.sum() + r.sum() }
