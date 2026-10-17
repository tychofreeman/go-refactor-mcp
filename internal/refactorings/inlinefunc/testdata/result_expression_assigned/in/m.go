package m

import "fmt"

func run(a, b int) {
	var m int
	m = larger(a, b)
	fmt.Println(m)
}

func larger(x, y int) int {
	if x > y {
		y = x
	}
	return y * 1
}
