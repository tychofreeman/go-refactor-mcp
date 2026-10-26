package m

import "fmt"

func load() int {
	println("load")
	return 3
}
func other() int {
	println("other")
	return 4
}

func F() int {
	v := load()
	fmt.Println("loaded")
	return v
}
