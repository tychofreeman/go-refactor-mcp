package m

type User struct {
	Name  string
	Email string
}

func (u User) email() string { return u.Email }

func greet(u User) string { return "hi " + u.email() }

func first(us []User) string {
	if len(us) == 0 {
		return ""
	}
	return us[0].email()
}

func set(u *User) { u.Email = "x" }
