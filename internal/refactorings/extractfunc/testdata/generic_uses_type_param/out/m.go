package m

import "fmt"

func run[T any]() {
	step[T]()
}

func step[T any]() {
	var zero T
	fmt.Println(zero)
}
