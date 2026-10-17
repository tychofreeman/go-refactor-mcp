package m

func head[T any](xs []T, n int) T { return xs[0] }

var X = head([]string{"a"}, 2)
