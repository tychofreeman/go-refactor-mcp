package m

import "fmt"

func run(ch chan int) {
	select {
	case v := <-ch:
		fmt.Println(v)
	default:
	}
}
