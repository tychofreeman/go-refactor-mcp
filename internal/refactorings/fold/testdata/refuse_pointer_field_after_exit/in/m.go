package m

import "log"

type R struct{ Errs []string }

func add(errs *[]string, msg string) {
	log.Fatalf("error: %s", msg)
	*errs = append(*errs, msg)
}

func (r *R) Add(msg string) {
	log.Fatalf("error: %s", msg)
	r.Errs = append(r.Errs, msg)
}
