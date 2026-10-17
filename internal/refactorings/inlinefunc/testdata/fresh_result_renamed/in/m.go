package m

import "fmt"

func run() {
	total := sum([]int{1, 2, 3})
	fmt.Println(total)
}

func sum(xs []int) int {
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}
