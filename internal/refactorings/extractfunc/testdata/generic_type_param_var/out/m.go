package m

import "fmt"

func run[T any](v T) {
	step[T](v)
}

func step[T any](v T) {
	fmt.Println(v)
}
