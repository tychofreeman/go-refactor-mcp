package m

import "fmt"

func count(xs []int) int {
	n := 0
	for _, x := range xs {
		if x > 0 {
			n += x
			fmt.Println("added", x)
		}
	}
	fmt.Println(n)
	return n
}
