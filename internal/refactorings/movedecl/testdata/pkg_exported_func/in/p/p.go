package p

import "strings"

// Shout upper-cases s.
func Shout(s string) string { return strings.ToUpper(s) + "!" }

func Greet(name string) string { return Shout("hi " + name) }
