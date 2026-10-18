package m

// job is one unit of work.
type job struct {
	// name says what it is.
	name  string
	owner string // who asked
	tries int
}

func newJob(n string) job { return job{name: n} }
