package m

import "fmt"

func run() {
	x := 1
	p := &x
	x = 2
	fmt.Println(*p)
}
