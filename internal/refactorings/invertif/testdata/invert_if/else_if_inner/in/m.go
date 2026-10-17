package m

import "fmt"

func run(a, b int) {
	if a == 0 {
		fmt.Println("zero")
	} else if b > 0 {
		fmt.Println("positive")
	} else {
		fmt.Println("other")
	}
}
