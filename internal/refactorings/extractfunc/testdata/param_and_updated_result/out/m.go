package m

import "fmt"

func run(xs []int) {
	total := 0
	total = sumDoubled(xs, total)
	fmt.Println(total)
}

func sumDoubled(xs []int, total int) int {
	for _, x := range xs {
		total += x
	}
	total *= 2
	return total
}
