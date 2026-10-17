package m

func first[T any](xs []T, fallback bool) T { return xs[0] }

func Use() int { return first([]int{1}, false) + first[int]([]int{2}, false) }
