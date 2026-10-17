package m

import "fmt"

func run() {
	x := 1
	show(x + 1)
}

func show(v int) {
	x := 10
	fmt.Println(x, v)
}
