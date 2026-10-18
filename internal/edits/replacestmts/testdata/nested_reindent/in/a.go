package m

import "fmt"

func count(xs []int) int {
	n := 0
	for _, x := range xs {
		if x > 0 {
			// count it
			n++
		}
	}
	fmt.Println(n)
	return n
}
