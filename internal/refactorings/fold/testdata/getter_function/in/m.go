package m

// User is someone to write to.
type User struct {
	Name  string
	Email string
}

func email(u User) string { return u.Email }

func greet(u User, m string) string { return u.Email + ": " + m }

func both(a, b User) []string { return []string{a.Email, b.Email, a.Name} }
