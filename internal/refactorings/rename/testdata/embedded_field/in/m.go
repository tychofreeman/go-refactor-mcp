package m

type Address struct {
	Street string
	City   string
}

func (a *Address) Line() string { return a.Street + ", " + a.City }

type Member struct {
	ID string
	// Address is where the member lives.
	Address
}

type Club struct {
	Member
}

func Describe(m Member, c *Club) []string {
	m.City = "Oslo"
	n := Member{ID: "b", Address: Address{City: "Bergen"}}
	return []string{m.Line(), n.City, c.City, c.Address.Street, c.Member.Street, m.Address.City}
}
