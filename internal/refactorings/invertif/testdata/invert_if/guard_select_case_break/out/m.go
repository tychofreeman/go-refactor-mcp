package m

import "fmt"

func run(ch chan int, verbose bool) {
	select {
	case v := <-ch:
		if !verbose {
			break
		}
		fmt.Println(v)
	default:
	}
}
