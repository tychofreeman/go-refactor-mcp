package m

import "fmt"

func run(x, y float64) {
	if x < y {
		fmt.Println("then")
	} else {
		fmt.Println("else")
	}
}
