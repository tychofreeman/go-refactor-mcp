package m

type Loader struct{ n int }

func sortBy(xs []string, less func(a, b string) bool) {}

func (l *Loader) run(xs []string) {
	sortBy(xs, l.less)
	sortBy(xs, byLength)
}

func (l *Loader) less(a string, b string) bool {
	panic("not implemented")
}

func byLength(a string, b string) bool {
	panic("not implemented")
}
