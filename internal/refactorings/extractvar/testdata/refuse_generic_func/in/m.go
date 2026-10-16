package m

func Map[T, U any](xs []T, f func(T) U) []U {
	out := make([]U, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

func itoa(i int) string { return "" }

func Strs(xs []int) []string {
	return Map(xs, itoa)
}
