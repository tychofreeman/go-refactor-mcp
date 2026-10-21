package p

type User struct {
	ID    int
	Email string
}

func WithEmail(u User, v string) User {
	u.Email = v
	return u
}
