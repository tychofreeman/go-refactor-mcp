package m

// User is someone to write to.
type User struct {
	Name  string
	Email string
}

func email(u User) string { return u.Email }

func greet(u User, m string) string { return email(u) + ": " + m }

func both(a, b User) []string { return []string{email(a), email(b), a.Name} }
