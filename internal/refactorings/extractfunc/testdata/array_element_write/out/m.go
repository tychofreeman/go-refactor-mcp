package m

import "fmt"

func run() {
	var xs [2]int
	xs = fill(xs)
	fmt.Println(xs)
}

func fill(xs [2]int) [2]int {
	xs[0] = 1
	xs[1] = 2
	return xs
}
