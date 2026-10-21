package m

type UserID string

type User struct {
	ID   string
	Name string
}

func lookup(id string) bool { return id != "" }

func f(u *User, s string) bool {
	u.ID = s
	v := User{ID: "x", Name: "n"}
	w := User{s, "n"}
	return lookup(u.ID) && v.ID == "x" && len(w.ID) > 0
}
