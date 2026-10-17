package m

import "fmt"

func run(v int) {
	ok := outside(v, v)
	fmt.Println(ok)
}

func outside(a, b int) bool {
	fmt.Println("checking")
	return a != 1 || b != 2
}
