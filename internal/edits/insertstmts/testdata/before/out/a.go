package m

import "fmt"

func count(xs []int) int {
	n := 0
	fmt.Println("counting", len(xs))
	for _, x := range xs {
		if x > 0 {
			n++
		}
	}
	return n
}
