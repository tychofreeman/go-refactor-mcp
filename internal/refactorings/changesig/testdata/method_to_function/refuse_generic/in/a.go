package m

type stack[T any] struct{ items []T }

func (s *stack[T]) size() int { return len(s.items) }
