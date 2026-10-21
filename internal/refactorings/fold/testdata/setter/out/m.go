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

func one(id int, v string) User { return withEmail(User{ID: id}, v) }

func many(v string) []User {
	return []User{
		withEmail(User{ID: 1}, v),
		withEmail(User{
			ID:   2,
			Name: "B",
		}, "b@x"),
		{ID: 3},
	}
}

func last() User {
	return withEmail(User{
		ID: 5,
	}, "e@x")
}

func only() User { return withEmail(User{}, "a@x") }

func ptr() *User { return &User{ID: 4, Email: "d@x"} }

func use(u User) string { return u.Email }

func call() string { return use(withEmail(User{Name: "n"}, "c@x")) }
