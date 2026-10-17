package m

func first[T any](xs []T) T { return xs[0] }

func Use() int { return first([]int{1}) + first[int]([]int{2}) }
