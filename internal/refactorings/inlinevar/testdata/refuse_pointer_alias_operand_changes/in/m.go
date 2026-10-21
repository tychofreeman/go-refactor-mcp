package m

type Point struct{ X, Y int }

type Box struct{ Min Point }

func Move(b, other *Box) {
	m := &b.Min
	b = other
	m.X = 1
}
