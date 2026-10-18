package m

import "strings"

func Greet() string { return shout("hi") }

func shout(s string) string { return strings.ToUpper(s) }
