package m

type Stack[T any] struct{ items []T }

func (s *Stack[T]) Push(v T) { s.items = append(s.items, v) }

func (s *Stack[T]) PushAll(xs []T) {
	panic("not implemented")
}

func (s *Stack[T]) At(n int) T {
	panic("not implemented")
}
