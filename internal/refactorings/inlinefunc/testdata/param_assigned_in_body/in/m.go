package m

import "fmt"

func run(n int) {
	countdown(n)
	fmt.Println(n)
}

func countdown(i int) {
	for i > 0 {
		fmt.Println(i)
		i--
	}
}
