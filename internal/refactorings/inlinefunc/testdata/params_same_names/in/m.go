package m

import "fmt"

func run(a, b int) {
	show(a, b)
}

func show(a int, b int) {
	fmt.Println(a + b)
	fmt.Println(a * b)
}
