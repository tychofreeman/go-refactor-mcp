package m

import "fmt"

func run() func() {
	return func() {
		printAB()
	}
}

func printAB() {
	fmt.Println("a")
	fmt.Println("b")
}
