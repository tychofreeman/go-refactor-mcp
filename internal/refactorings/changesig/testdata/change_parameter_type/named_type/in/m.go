package m

import "strings"

type UserID string

func (u UserID) String() string { return "user:" + string(u) }

func Greet(id string, times int) string {
	if id == "" || len(id) > 10 {
		return strings.Repeat("hi ", times)
	}
	return strings.Repeat("hi "+id, times)
}

func Run(name string) string {
	return Greet(name, 2) + Greet("bob", 1)
}
