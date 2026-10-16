package m

import "fmt"

func run() {
	f := func(n int) int {
		return n * 2
	}
	fmt.Println(f(3))
}
