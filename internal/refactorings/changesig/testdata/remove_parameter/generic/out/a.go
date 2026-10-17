package m

func head[T any](xs []T) T { return xs[0] }

var X = head([]string{"a"})
