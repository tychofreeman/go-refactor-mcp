package m

// Runner runs a step.
type Runner interface{ Run() error }

// Step is a named runner.
type Step struct {
	Name string
	Run  Runner
}

func steps() []Step {
	return []Step{{Name: "a", Run: newRunner()}}
}

func runners() []Runner { return []Runner{newOther()} }

func newRunner() Runner {
	panic("not implemented")
}

func newOther() Runner {
	panic("not implemented")
}
