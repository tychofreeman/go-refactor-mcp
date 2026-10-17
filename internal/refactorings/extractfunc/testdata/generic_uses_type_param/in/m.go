package m

import "fmt"

func run[T any]() {
	var zero T
	fmt.Println(zero)
}
