package m

type Address struct{ City string }

func (a Address) String() string { return a.City }

type Member struct {
	ID string
	Address
}

func City(m Member) string { return m.City }
