package m

import "fmt"

func run() {
	defer fmt.Println("bye")
	fmt.Println("hi")
}
