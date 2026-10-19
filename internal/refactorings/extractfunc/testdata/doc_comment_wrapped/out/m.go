package m

import "fmt"

func run() {
	fmt.Println("start")
	middle()
	fmt.Println("end")
}

// middle prints the middle lines, the ones that come after start and before
// end, in the order they are given.
func middle() {
	fmt.Println("one")
	fmt.Println("two")
}
