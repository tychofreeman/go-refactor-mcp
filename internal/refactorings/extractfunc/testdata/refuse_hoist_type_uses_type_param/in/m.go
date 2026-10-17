package m

import "fmt"

func run[T any](v T) {
	type box struct{ v T }
	b := box{v}
	fmt.Println(b)
}
