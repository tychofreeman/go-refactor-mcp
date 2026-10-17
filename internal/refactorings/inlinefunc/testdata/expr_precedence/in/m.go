package m

import "fmt"

func run(a, b int) {
	fmt.Println(2 * sum(a, b))
}

func sum(x, y int) int {
	return x + y
}
