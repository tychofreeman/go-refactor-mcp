package m

type Box[T any] struct{ val T }

func (b Box[T]) Get() T { return b.val }

func Map[In, U any](xs []In, f func(In) U) []U {
	out := make([]U, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}
