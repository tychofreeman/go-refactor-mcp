package m

import "fmt"

func run() {
	xs := make([]int, 2)
	fill(xs)
	fmt.Println(xs)
}

func fill(xs []int) {
	xs[0] = 1
	xs[1] = 2
}
