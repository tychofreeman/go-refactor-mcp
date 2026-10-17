package m

type point struct{ x, y int }

func (p point) Moved() point {
	p.x++
	p.y++
	return p
}
