package m

type P struct{ x, y int }

func Move(p P) P {
	p.x = 3
	return p
}
