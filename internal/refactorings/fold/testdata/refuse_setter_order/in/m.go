package m

type User struct {
	Email string
	Name  string
}

var n int

func next() string {
	n++
	return "x"
}

func withEmail(u User, v string) User {
	u.Email = v
	return u
}

func make2() User { return User{Email: next(), Name: next()} }
