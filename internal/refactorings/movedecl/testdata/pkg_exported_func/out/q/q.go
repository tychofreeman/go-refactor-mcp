package q

import "strings"

func Base() int { return 1 }

// Shout upper-cases s.
func Shout(s string) string { return strings.ToUpper(s) + "!" }
