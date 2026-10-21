package m

type User struct {
	Name     string
	Contacts map[string]string
}

func (u User) Contact(k string) string { return u.Contacts[k] }
