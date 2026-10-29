package m

import "fmt"

type user struct {
	name  string
	email string
}

func show(u user) string { return fmt.Sprint(u) }
