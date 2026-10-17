package m

import "fmt"

func run(a, b int) {
	fmt.Println(square(a+b) + 1)
}

func square(x int) int {
	return x * x
}
