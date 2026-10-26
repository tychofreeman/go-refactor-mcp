package m

type User struct {
	Email string
	Name  string
}

type Form struct{ name string }

var n int

func next() string {
	n++
	return "x"
}

func withEmail(u User, v string) User {
	u.Email = v
	return u
}

func make2(f *Form) User { return User{Email: next(), Name: f.name} }
