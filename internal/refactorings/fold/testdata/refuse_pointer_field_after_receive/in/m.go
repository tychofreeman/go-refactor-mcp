package m

type R struct{ Errs []string }

func add(errs *[]string, ready chan string) {
	msg := <-ready
	*errs = append(*errs, msg)
}

func (r *R) Add(ready chan string) {
	s := <-ready
	r.Errs = append(r.Errs, s)
}
