package m

import "fmt"

// run prints a pair.
func run() {
	type pair struct{ a, b int }
	p := pair{1, 2}
	fmt.Println(p.a)
	fmt.Println(p.b)
}
