package m

type User struct {
	ID    int
	Email string
}

func (u User) WithEmail(v string) User {
	u.Email = v
	return u
}

func make1(v string) User { return User{ID: 1}.WithEmail(v) }

func same(u User, v string) bool {
	if u == (User{ID: 1}.WithEmail(v)) {
		return true
	}
	return false
}

var admins = []User{User{ID: 0}.WithEmail("root@x")}
