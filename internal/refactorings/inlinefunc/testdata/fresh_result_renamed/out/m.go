package m

import "fmt"

func run() {
	total := 0
	for _, x := range []int{1, 2, 3} {
		total += x
	}
	fmt.Println(total)
}
