package m

import "fmt"

func Water(k int) {
	n := 1
again:
	fmt.Println(n)
	n = 3
	fmt.Println(n)
	k--
	if k > 0 {
		goto again
	}
}
