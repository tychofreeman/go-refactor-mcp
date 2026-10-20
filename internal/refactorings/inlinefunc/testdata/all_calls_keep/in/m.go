package m

import "fmt"

func run() {
	greet("a")
	greet("b")
}

func greet(name string) {
	fmt.Println("hello", name)
}
