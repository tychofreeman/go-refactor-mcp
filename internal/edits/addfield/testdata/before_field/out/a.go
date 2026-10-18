package m

// job is one unit of work.
type job struct {
	id int
	// name says what it is.
	name  string
	tries int
}

func newJob(n string) job { return job{name: n} }
