package m

type Getter interface {
	Get(i int) string
}

type List[T any] struct{ items []T }

var _ Getter = List[string]{}

func (l List[T]) Get(i int) T {
	panic("not implemented")
}
