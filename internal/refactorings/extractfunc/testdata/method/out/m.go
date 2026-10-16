package m

import "fmt"

type T struct{}

func (T) Run() {
	printAB()
}

func printAB() {
	fmt.Println("a")
	fmt.Println("b")
}
