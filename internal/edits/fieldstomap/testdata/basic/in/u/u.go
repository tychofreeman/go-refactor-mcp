package u

// User is someone to reach.
type User struct {
	ID    string
	Email string // an address
	Phone string
	Age   int
}

// Reach returns where to reach x.
func Reach(x User) string {
	if x.Email != "" {
		return x.Email
	}
	return x.Phone
}
