package m

type pt struct{ x, y int }

func F() pt {
	var p pt
	p.x = 1
	p.y = 2
	return p
}
