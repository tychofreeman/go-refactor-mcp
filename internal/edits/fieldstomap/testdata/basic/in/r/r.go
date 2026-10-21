package r

import "example.com/m/u"

var Users = []u.User{
	{ID: "1", Email: "a@x", Phone: "5"},
	{ID: "2", Phone: "6", Age: 3},
	{ID: "3"},
}

func New(e string) *u.User {
	return &u.User{
		ID:    "4",
		Email: e,
		Age:   2,
	}
}

func Copy(v u.User) u.User {
	return u.User{Email: v.Email}
}
