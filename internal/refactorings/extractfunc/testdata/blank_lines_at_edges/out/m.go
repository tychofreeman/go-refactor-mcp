package m

import "fmt"

func run() {
	fmt.Println("a")

	printB()

	fmt.Println("c")
}

func printB() {
	fmt.Println("b")
}
