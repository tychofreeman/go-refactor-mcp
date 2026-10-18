package m

import "fmt"

func F(xs []int) int {
	a := xs[0]
	defer fmt.Println("done")
	return a
}
