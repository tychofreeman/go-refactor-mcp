package m

type base struct{ id int }

func (base) ok() bool { return true }

type user struct {
	base
	name string
}

var _ = user{}.ok
