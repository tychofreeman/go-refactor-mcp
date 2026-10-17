package m

import "fmt"

func run(xs []int) {
	total := 0
	for _, x := range xs {
		if x <= 0 {
			continue
		}
		fmt.Println(x)
		total += x
	}
	fmt.Println(total)
}
