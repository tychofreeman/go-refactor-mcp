package m

func pick[T any](xs []T, i int) T { return xs[i] }

var X = pick([]string{"a"}, 0)
