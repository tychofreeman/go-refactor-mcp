package m

func pick[T any](i int, xs []T) T { return xs[i] }

var X = pick(0, []string{"a"})
