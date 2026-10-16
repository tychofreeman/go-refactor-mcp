package m

import "fmt"

func run() {
	fmt.Println("start")

	greet()

	fmt.Println("end")
}

func greet() {
	// Say hello twice.
	fmt.Println("hello") // first
	fmt.Println("hello") // second
}
