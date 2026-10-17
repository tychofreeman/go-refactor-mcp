package m

import "fmt"

func run(ch chan int, verbose bool) {
	select {
	case v := <-ch:
		if verbose {
			fmt.Println(v)
		}
	default:
	}
}
