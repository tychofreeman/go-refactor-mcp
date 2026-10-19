package p

import "example.com/m/q"

// Other stays, and still needs helper.
func Other() int { return q.Helper() }
