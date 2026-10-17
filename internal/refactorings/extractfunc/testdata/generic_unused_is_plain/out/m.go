package m

import "fmt"

func show[T any](v T) {
	banner()
	fmt.Println(v)
}

func banner() {
	fmt.Println("showing")
}
