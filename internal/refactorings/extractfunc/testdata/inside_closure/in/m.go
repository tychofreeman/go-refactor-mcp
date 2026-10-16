package m

import "fmt"

func run() func() {
	return func() {
		fmt.Println("a")
		fmt.Println("b")
	}
}
