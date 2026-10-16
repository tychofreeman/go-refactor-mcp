package m

import "fmt"

func run() {
	double()
}

func double() {
	f := func(n int) int {
		return n * 2
	}
	fmt.Println(f(3))
}
