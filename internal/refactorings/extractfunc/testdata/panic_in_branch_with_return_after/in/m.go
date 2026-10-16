package m

func run(ok bool) int {
	if !ok {
		panic("bad")
	}
	return 1
}
