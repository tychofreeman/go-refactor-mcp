package m

func cleanup() error { return nil }

func F() {
	defer cleanup()
}
