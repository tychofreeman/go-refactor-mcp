package m

type Address struct{ City string }

type Member struct {
	ID string
	Address
}

type Club struct {
	Member
	Home string
}

func City(c Club) string { return c.City }
