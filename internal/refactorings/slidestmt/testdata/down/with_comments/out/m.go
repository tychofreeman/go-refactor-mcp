package m

import "fmt"

func F(x int) {
	// b is the second.
	b := x * 2

	// a is the first.
	a := x + 1 // trailing
	fmt.Println(a, b)
}
