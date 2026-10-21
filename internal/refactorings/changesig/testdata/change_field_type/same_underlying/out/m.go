package m

type UserID string

type User struct {
	ID   UserID
	Name string
}

func lookup(id string) bool { return id != "" }

func f(u *User, s string) bool {
	u.ID = UserID(s)
	v := User{ID: "x", Name: "n"}
	w := User{UserID(s), "n"}
	return lookup(string(u.ID)) && v.ID == "x" && len(w.ID) > 0
}
