package m

import "fmt"

func run(a, b int) {
	if a == 1 && b == 2 {
		fmt.Println("else")
	} else {
		fmt.Println("then")
	}
}
