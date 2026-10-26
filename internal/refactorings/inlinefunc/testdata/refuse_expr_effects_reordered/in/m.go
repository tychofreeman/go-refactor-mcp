package m

import "fmt"

func a() int {
	println("a")
	return 1
}
func b() int {
	println("b")
	return 2
}

func run() {
	fmt.Println(sub(a(), b()))
}

func sub(x, y int) int {
	return y - x
}
