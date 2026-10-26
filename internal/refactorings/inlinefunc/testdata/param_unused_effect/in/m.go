package m

import "fmt"

func load() int {
	println("load")
	return 1
}

func run() {
	ignore(load(), 2)
}

func ignore(_ int, b int) {
	fmt.Println(b)
}
