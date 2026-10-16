package m

import "fmt"

func pick(xs []int) int {
	for _, x := range xs {
		if x > 10 {
			return big(x)
		}
	}
	return 0
}

func big(x int) int {
	fmt.Println("big", x)
	return x * 2
}
