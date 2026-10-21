package m

type User struct {
	ID    int
	Name  string
	Email string
}

func withEmail(u User, v string) User {
	u.Email = v
	return u
}

func one(id int, v string) User { return User{ID: id, Email: v} }

func many(v string) []User {
	return []User{
		{ID: 1, Email: v},
		{
			ID:    2,
			Email: "b@x",
			Name:  "B",
		},
		{ID: 3},
	}
}

func last() User {
	return User{
		ID:    5,
		Email: "e@x",
	}
}

func only() User { return User{Email: "a@x"} }

func ptr() *User { return &User{ID: 4, Email: "d@x"} }

func use(u User) string { return u.Email }

func call() string { return use(User{Name: "n", Email: "c@x"}) }
