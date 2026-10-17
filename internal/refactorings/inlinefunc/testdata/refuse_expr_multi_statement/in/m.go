package m

import "fmt"

func run(a int) {
	fmt.Println(twice(a) + 1)
}

func twice(x int) int {
	y := x * 2
	return y
}
