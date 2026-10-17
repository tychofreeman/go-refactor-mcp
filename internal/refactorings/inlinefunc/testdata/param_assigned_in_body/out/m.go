package m

import "fmt"

func run(n int) {
	i := n
	for i > 0 {
		fmt.Println(i)
		i--
	}
	fmt.Println(n)
}
