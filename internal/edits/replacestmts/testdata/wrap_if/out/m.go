package m

import "fmt"

func Water(verbose bool, n int) int {
	n++
	if verbose {
		fmt.Println("watered", n)
		// done
	}
	return n
}
