package m

func run(limit int) func(int) bool {
	return func(v int) bool { return v < limit }
}
