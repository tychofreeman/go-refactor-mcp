package m

import "fmt"

func run() {
	n := 1
	n = bump(n)
	fmt.Println(n)
}

func bump(n int) int {
	func() {
		n *= 3
	}()
	return n
}
