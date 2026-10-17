package m

func first[T any](xs []T, def T) T {
	return pick[T](xs, def)
}

func pick[T any](xs []T, def T) T {
	if len(xs) == 0 {
		return def
	}
	return xs[0]
}
