package m

type Stack[T any] struct{ items []T }

func (s *Stack[T]) PushTwice(v T) {
	s.items = append(s.items, v)
	s.items = append(s.items, v)
}
