package m

func run(ok bool) int {
	if !ok {
		fail()
	}
	return 1
}

func fail() {
	panic("bad")
}
