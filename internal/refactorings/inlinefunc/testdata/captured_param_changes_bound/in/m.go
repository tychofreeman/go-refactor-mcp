package m

import "fmt"

func run() {
	n := 1
	show(n)
	n = 2
	fmt.Println(n)
}

func show(n int) {
	f := func() { fmt.Println(n) }
	f()
}
