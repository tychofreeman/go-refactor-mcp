package m

func run(ok bool) int {
	if ok {
		return 1
	} else {
		return step()
	}
}

func step() int {
	panic("bad")
}
