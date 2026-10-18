package m

var count int

func F() int {
	c := count
	bump()
	return c
}

func bump() { count++ }
