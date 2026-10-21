package m

type Point struct {
	X, Y int
	Name string
}

func origin() Point { return Point{X: 0, Y: 0, Name: "o"} }
