package m

import "fmt"

func run() {
	n := step()
	fmt.Println(n)
}

func step() int {
	n := 3
	fmt.Println("x")
	return n
}
