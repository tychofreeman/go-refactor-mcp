package m

import "fmt"

var mode int

func run() {
	switch mode {
	case 1:
		fmt.Println("one")
		fmt.Println("uno")
	default:
		fmt.Println("other")
	}
}
