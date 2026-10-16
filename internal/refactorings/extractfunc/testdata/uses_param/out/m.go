package m

import "fmt"

func run(name string) {
	step(name)
}

func step(name string) {
	fmt.Println("hello")
	fmt.Println(name)
}
