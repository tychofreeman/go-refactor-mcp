package m

type Plant struct {
	name string
	old  int
	age  int
}

var P = Plant{"fern", len("x") + n, 3}

var n = 2

var Q = []Plant{{"moss", 0, 1}}

func (p Plant) Age() int { return p.age }
