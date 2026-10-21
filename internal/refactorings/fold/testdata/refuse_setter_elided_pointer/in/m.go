package m

type User struct {
	Email string
}

func withEmail(u User, v string) User {
	u.Email = v
	return u
}

var users = []*User{{Email: "a@x"}}
