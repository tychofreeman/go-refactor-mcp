package m

type Address struct{ City string }

func (a Address) line() string { return a.City }

type Member struct {
	ID string
	Address
}

var f = Member.line
