package c

import (
	"example.com/m/a"
	"example.com/m/b"
)

// Twice runs 2.
func Twice() int { return a.Dispatch(b.Make(), 2) }
