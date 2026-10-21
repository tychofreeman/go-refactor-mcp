package m

type Point struct{ X, Y int }

type Box struct{ Min Point }

func Grow(b *Box) Point {
	m := &b.Min
	m.X--
	(*m).Y--
	return *m
}
