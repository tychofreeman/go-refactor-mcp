package m

type Plant struct {
	name string
	old  int
	age  int
}

var P = Plant{"fern", 0, 3}

var Q = []Plant{{"moss", 0, 1}}

func (p Plant) Age() int { return p.age }
