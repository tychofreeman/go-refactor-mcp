package m

import "fmt"

func run() {
	x := 1
	if true {
		fmt.Println(x)
		x := 2
		fmt.Println(x)
	}
	fmt.Println(x)
}
