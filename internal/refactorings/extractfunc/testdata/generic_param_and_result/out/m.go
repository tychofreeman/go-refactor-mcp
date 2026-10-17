package m

import "fmt"

func sum[T int | float64](xs []T) T {
	var total T
	total = add[T](xs, total)
	fmt.Println(total)
	return total
}

func add[T int | float64](xs []T, total T) T {
	for _, x := range xs {
		total += x
	}
	return total
}
