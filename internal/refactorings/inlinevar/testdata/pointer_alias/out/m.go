package m

type Point struct{ X, Y int }

type Box struct{ Min Point }

func Grow(b *Box) Point {
	b.Min.X--
	b.Min.Y--
	return b.Min
}
