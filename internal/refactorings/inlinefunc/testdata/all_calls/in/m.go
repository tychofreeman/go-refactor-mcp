package m

import "fmt"

func run() {
	greet("a")
	fmt.Println("between")
	greet("b")
}

// greet says hello.
func greet(name string) {
	fmt.Println("hello", name)
}
