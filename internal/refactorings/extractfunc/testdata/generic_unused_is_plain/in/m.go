package m

import "fmt"

func show[T any](v T) {
	fmt.Println("showing")
	fmt.Println(v)
}
