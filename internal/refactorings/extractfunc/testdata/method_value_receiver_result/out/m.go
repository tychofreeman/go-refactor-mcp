package m

type point struct{ x, y int }

func (p point) Moved() point {
	p = p.shift()
	return p
}

func (p point) shift() point {
	p.x++
	p.y++
	return p
}
