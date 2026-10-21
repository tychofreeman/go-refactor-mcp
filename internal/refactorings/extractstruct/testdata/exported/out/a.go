package m

type xy struct {
	X, Y int
}

type Point struct {
	xy
	Name string
}

func origin() Point { return Point{xy: xy{X: 0, Y: 0}, Name: "o"} }
