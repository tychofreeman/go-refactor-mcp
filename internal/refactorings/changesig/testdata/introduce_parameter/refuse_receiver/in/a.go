package m

type point struct{ x, y int }

func (p point) label() int {
	return p.x
}

func Use() int { return point{}.label() }
