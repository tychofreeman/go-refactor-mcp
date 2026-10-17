package m

import "fmt"

func run(xs []int) {
	for _, x := range xs {
		if x > 0 {
			fmt.Println("value")
			fmt.Println(x)
		}
	}
}
