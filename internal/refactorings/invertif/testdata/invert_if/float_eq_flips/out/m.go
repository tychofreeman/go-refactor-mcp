package m

import "fmt"

func run(x, y float64) {
	if x != y {
		fmt.Println("else")
	} else {
		fmt.Println("then")
	}
}
