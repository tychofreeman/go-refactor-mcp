package m

type User struct {
	Name  string
	Email string
}

func (u User) email() string { return u.Email }

func greet(u User) string { return "hi " + u.Email }

func bye(u User) string { return "bye " + u.Email }
