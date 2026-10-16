package m

import "fmt"

func run(xs []int) {
	last := 0
	for _, x := range xs {
		last = compare(x, last)
	}
}

func compare(x int, last int) int {
	if x > last {
		fmt.Println("up")
	}
	last = x
	return last
}
