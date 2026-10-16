package m

func run(n int) int {
	switch n {
	case 1:
		return 1
	default:
		return step()
	}
}

func step() int {
	panic("bad")
}
