package m

import "fmt"

func pick(xs []int) int {
	for _, x := range xs {
		if x > 10 {
			fmt.Println("big", x)
			return x * 2
		}
	}
	return 0
}
