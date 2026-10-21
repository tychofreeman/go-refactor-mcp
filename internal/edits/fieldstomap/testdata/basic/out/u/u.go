package u

// User is someone to reach.
type User struct {
	ID       string
	Contacts map[string]string
	Age      int
}

// Reach returns where to reach x.
func Reach(x User) string {
	if x.Contacts["email"] != "" {
		return x.Contacts["email"]
	}
	return x.Contacts["sms"]
}
