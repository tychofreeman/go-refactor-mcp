package m

import "fmt"

func run(xs []int) {
	for _, x := range xs {
		if x <= 0 {
			continue
		}
		y := x * 2
		fmt.Println(y)
		fmt.Println("next")
	}
}
