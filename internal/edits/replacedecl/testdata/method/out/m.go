package m

type point struct{ x, y int }

func (p point) sum() int {
	s := p.x
	return s + p.y
}

func (p point) diff() int { return p.x - p.y }
