package m

func run(ok bool) int {
	if done, v := step(ok); done {
		return v
	}
	return 0
}

func step(ok bool) (bool, int) {
	if ok {
		return true, 1
	}
	return false, 0
}
