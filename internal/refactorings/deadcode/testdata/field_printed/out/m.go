package m

import "fmt"

type user struct {
	name string
}

func show(u user) string { return fmt.Sprint(u) }
