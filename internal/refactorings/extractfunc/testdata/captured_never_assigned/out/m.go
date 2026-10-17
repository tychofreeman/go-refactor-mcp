package m

import "fmt"

func run() {
	n := 1
	f := step(n)
	f()
}

func step(n int) func() {
	f := func() {
		fmt.Println(n)
	}
	return f
}
