package m

type mover struct {
	path string
	n    int
}

func make1() mover {
	return mover{
		// the path
		path: "p",
		n:    1,
	}
}
