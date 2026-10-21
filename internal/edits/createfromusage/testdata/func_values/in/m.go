package m

type Loader struct{ n int }

func sortBy(xs []string, less func(a, b string) bool) {}

func (l *Loader) run(xs []string) {
	sortBy(xs, l.less)
	sortBy(xs, byLength)
}
