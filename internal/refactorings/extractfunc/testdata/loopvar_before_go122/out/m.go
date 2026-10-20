package m

import "fmt"

func run(xs []int, done chan bool) {
	for _, x := range xs {
		start(&x, done)
	}
}

func start(x *int, done chan bool) {
	go func() {
		fmt.Println(*x)
		done <- true
	}()
}
