package m

import "fmt"

func run(xs []int) {
	last := 0
	for _, x := range xs {
		if x > last {
			fmt.Println("up")
		}
		last = x
	}
	fmt.Println(last)
}
