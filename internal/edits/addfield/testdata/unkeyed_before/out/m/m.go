package m

type Point struct{ X, Y int }

type Plant struct {
	Name   string
	Spot   Point
	Tags   []string
	Height int
}

var fern = Plant{"fern", Point{}, nil, 3}

var keyed = Plant{Name: "moss"}
