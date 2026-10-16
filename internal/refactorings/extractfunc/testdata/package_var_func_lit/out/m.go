package m

import "fmt"

var handler = func() {
	printAB()
}

func printAB() {
	fmt.Println("a")
	fmt.Println("b")
}

func other() {}
