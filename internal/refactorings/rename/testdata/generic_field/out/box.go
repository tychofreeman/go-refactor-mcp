package m

type Box[T any] struct{ value T }

func (b Box[T]) Get() T { return b.value }

func Use() (int, string) {
	i := Box[int]{value: 3}
	s := Box[string]{value: "x"}
	return i.Get(), s.value
}
