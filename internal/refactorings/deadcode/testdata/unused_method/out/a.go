package m

type point struct{ x int }

func (p point) X() int { return p.x }

func Use() int { return point{}.X() }
