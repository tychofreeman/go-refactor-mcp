package m

import "fmt"

func run(a, b int) {
	if a != 0 && b != 0 {
		fmt.Println("else")
	} else {
		fmt.Println("then")
	}
}
