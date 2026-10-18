package m

var x = 1

func F() int {
	y := x
	x := 2
	return y + x
}
