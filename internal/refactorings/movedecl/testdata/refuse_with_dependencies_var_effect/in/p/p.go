package p

import "os"

var greeting = os.Getenv("GREETING")

// Greet greets.
func Greet() string { return greeting }

// Other stays.
func Other() int { return 7 }
