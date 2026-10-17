package p

import "example.com/m/q"

func Greet(name string) string { return q.Shout("hi " + name) }
