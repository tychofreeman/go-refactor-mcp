package m

type Box[T any] struct{ val T }

func (b Box[T]) Get() T { return b.val }

func Use() (int, string) {
	i := Box[int]{val: 3}
	s := Box[string]{val: "x"}
	return i.Get(), s.val
}
