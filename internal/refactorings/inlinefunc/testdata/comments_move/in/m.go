package m

import "fmt"

func run() {
	greet("bob") // say hi
}

func greet(name string) {
	// Build the greeting.
	msg := "hi " + name

	fmt.Println(msg) // print it
}
