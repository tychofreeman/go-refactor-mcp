package m

type point struct{ x, y int }

func (p point) sum() int { return p.x + p.y }
