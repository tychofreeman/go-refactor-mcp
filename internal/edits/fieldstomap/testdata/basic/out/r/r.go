package r

import "example.com/m/u"

var Users = []u.User{
	{ID: "1", Contacts: map[string]string{"email": "a@x", "sms": "5"}},
	{ID: "2", Contacts: map[string]string{"sms": "6"}, Age: 3},
	{ID: "3"},
}

func New(e string) *u.User {
	return &u.User{
		ID:       "4",
		Contacts: map[string]string{"email": e},
		Age:      2,
	}
}

func Copy(v u.User) u.User {
	return u.User{Contacts: map[string]string{"email": v.Contacts["email"]}}
}
