package p

import "strings"

// Base is where counting starts.
func Base() int { return 1 }

// Two is one past Base, and its name.
func Two() (int, string) { return Base() + 1, strings.ToUpper("two") }
