package m

type Plant struct {
	name string
	age  int
}

var P = Plant{"fern", 3}

var Q = []Plant{{"moss", 1}}

func (p Plant) Age() int { return p.age }
