package m

import "fmt"

func run(xs []int) {
	for _, x := range xs {
		if x < 0 {
			break
		}
		fmt.Println(x)
	}
}
