package m

type pt struct{ x, y int }

func F() pt {
	var p pt
	q := p
	p.x = 1
	return q
}
