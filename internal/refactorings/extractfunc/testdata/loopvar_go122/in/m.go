package m

import "fmt"

func run(xs []int, done chan bool) {
	for _, x := range xs {
		go func() {
			fmt.Println(x)
			done <- true
		}()
	}
}
