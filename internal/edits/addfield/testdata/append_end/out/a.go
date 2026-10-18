package m

import "time"

// job is one unit of work.
type job struct {
	// name says what it is.
	name  string
	tries int
	// timeout limits each try.
	timeout time.Duration `custom:"timeout"`
}

func newJob(n string) job { return job{name: n} }
