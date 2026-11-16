package c

import "example.com/m/b"

// Twice runs 2.
func Twice() int { return b.Make().Dispatch(2) }
