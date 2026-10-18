package m

type point struct{ x, y int }

type none struct{}

func origin() point { return point{x: 0} }
