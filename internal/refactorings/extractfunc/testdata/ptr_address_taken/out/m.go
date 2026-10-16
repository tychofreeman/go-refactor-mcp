package m

import "fmt"

func run() {
	x := 1
	p := &x
	step(&x)
	fmt.Println(*p)
}

func step(x *int) {
	*x = 2
}
