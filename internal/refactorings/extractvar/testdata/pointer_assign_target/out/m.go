package m

type P struct{ x, y int }

func Move(p P) P {
	px := &p.x
	*px = 3
	return p
}
