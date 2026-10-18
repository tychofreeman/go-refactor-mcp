package m

type pt struct{ x, y int }

func F() pt {
	var p pt
	p.y = 2
	p.x = 1
	return p
}
