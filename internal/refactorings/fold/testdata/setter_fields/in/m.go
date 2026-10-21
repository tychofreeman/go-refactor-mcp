package m

import "strings"

type User struct {
	ID    int
	Email string
	Phone string
}

func contact(u User, email, phone string) User {
	u.Email = strings.ToLower(email)
	u.Phone = phone
	return u
}

func a() User { return User{ID: 1, Phone: "1", Email: strings.ToLower("A@X")} }

func b() User { return User{ID: 2, Email: "a@x", Phone: "2"} }

func c(e string) User { return User{Email: strings.ToLower(e)} }
