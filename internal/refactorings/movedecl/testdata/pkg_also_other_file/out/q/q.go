package q

import "strings"

func Base() int { return 1 }

// Shout upper-cases s and adds a bang.
func Shout(s string) string { return Bang(Upper(s)) }

func Upper(s string) string { return strings.ToUpper(s) }

func Bang(s string) string { return s + "!" }
