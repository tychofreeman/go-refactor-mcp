package m

type User struct{ Email string }

func Set(u *User) {
	u.Email = "a"
}
