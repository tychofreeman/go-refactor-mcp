package m

import "fmt"

func run(xs []int) {
	total := 0
	for _, x := range xs {
		total += x
	}
	total *= 2
	fmt.Println(total)
}
