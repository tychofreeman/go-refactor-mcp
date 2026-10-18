package p

import "strings"

// Up upper-cases s.
func Up(s string) string { return strings.ToUpper(s) }

func Keep() string { return Up("k") }
