package m

import "fmt"

func run() int {
	return step()
}

func step() int {
	fmt.Println("unreachable")
	panic("not implemented")
}
