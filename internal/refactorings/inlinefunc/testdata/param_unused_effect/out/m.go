package m

import "fmt"

func load() int {
	println("load")
	return 1
}

func run() {
	_ = load()
	fmt.Println(2)
}
