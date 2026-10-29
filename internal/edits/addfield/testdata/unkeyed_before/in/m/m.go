package m

type Point struct{ X, Y int }

type Plant struct {
	Name   string
	Height int
}

var fern = Plant{"fern", 3}

var keyed = Plant{Name: "moss"}
