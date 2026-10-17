package m

import "fmt"

func run(a, b int) {
	if !(a == 1 && b == 2) {
		fmt.Println("then")
	} else {
		fmt.Println("else")
	}
}
