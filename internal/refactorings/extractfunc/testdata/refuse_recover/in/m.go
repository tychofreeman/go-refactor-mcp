package m

import "fmt"

func run() {
	defer func() {
		r := recover()
		fmt.Println(r)
	}()
	panic(1)
}

func handler() {
	recover()
	fmt.Println("x")
}
