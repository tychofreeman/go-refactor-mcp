package m

// User is a user.
type User struct {
	ID int
	// email is where mail goes.
	email string // old
	name  string
}

func n(u User) string { return u.name }

func mk() User { return User{ID: 1, name: "a"} }
