package m

import "fmt"

func run() {
	n := 1
	func() {
		n *= 3
	}()
	fmt.Println(n)
}
