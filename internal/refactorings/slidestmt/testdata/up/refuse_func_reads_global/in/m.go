package m

import "fmt"

var count int

func get() int { return count }

func F() {
	fmt.Println("x")
	n := get()
	fmt.Println(n)
}
