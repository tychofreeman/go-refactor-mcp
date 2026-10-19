package sub

import "example.com/dep/strutil"

// Shout upper-cases s.
func Shout(s string) string { return strutil.Up(s) }
