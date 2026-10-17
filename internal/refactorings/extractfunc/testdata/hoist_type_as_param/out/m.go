package m

import "fmt"

type pair struct{ a, b int }

// run prints a pair.
func run() {
	p := pair{1, 2}
	show(p)
}

func show(p pair) {
	fmt.Println(p.a)
	fmt.Println(p.b)
}
