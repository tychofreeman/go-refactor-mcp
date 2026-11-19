package m

import "fmt"

type Plant struct{ Name string }

func (p Plant) String() string { return p.Name }

func label(p Plant) fmt.Stringer {
	q := p
	return q
}

func show(p Plant) Plant {
	q := p
	return q
}
