package m

type Liner interface{ Line() string }

type Address struct{ City string }

func (a Address) Line() string { return a.City }

type Member struct {
	ID string
	Address
}

func Print(l Liner) string { return l.Line() }
