package m

import "fmt"

type Namer interface {
	String() string
}

type user struct{ name string }

func (u user) String() string { return u.name }

var _ fmt.Stringer = user{}

func show(n Namer) string { return n.String() }
