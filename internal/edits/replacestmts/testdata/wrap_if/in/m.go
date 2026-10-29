package m

import "fmt"

func Water(verbose bool, n int) int {
	n++
	fmt.Println("watered", n)
	// done
	return n
}
