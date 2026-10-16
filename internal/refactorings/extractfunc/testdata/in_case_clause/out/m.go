package m

import "fmt"

var mode int

func run() {
	switch mode {
	case 1:
		sayOne()
	default:
		fmt.Println("other")
	}
}

func sayOne() {
	fmt.Println("one")
	fmt.Println("uno")
}
