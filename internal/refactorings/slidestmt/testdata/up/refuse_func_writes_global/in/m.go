package m

import "fmt"

var count int

func bump() { count++ }

func F(x int) {
	fmt.Println(x)
	bump()
}
