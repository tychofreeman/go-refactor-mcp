package m

import "fmt"

func run(xs []int) {
	for _, x := range xs {
		if x > 0 {
			y := x * 2
			fmt.Println(y)
		} else {
			continue
		}
		fmt.Println("next")
	}
}
