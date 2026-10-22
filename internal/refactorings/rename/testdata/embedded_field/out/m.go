package m

type Address struct {
	Street string
	City   string
}

func (a *Address) Line() string { return a.Street + ", " + a.City }

type Member struct {
	ID string
	// Address is where the member lives.
	Home Address
}

type Club struct {
	Member
}

func Describe(m Member, c *Club) []string {
	m.Home.City = "Oslo"
	n := Member{ID: "b", Home: Address{City: "Bergen"}}
	return []string{m.Home.Line(), n.Home.City, c.Home.City, c.Home.Street, c.Member.Home.Street, m.Home.City}
}
