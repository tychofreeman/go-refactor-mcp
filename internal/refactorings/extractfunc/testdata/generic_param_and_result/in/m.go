package m

import "fmt"

func sum[T int | float64](xs []T) T {
	var total T
	for _, x := range xs {
		total += x
	}
	fmt.Println(total)
	return total
}
