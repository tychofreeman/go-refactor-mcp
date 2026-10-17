package m

type point struct{ x, y int }

func f(p point) point {
	p.x = 3
	return p
}

func Use() point { return f(point{}) }
