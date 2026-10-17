package m

import "fmt"

func run(a, b int, ok bool) {
	if a == 1 || b == 2 && ok {
		fmt.Println("then")
	} else {
		fmt.Println("else")
	}
}
