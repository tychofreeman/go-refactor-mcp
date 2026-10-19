package m

type point struct{ x, y int }

func f(p point) int {
	return p.x
}

var made int

func next() point { made++; return point{1, 2} }

func Use() int { return f(next()) }
