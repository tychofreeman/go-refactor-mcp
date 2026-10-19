package p

import "fmt"

var greeting = fmt.Sprint("hi")

// Greet greets.
func Greet() string { return greeting }

// Other stays.
func Other() int { return 7 }
