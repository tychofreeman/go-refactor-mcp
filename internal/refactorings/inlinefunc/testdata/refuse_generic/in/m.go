package m

import "fmt"

func run() {
	show(1)
}

func show[T any](v T) {
	fmt.Println(v)
}
