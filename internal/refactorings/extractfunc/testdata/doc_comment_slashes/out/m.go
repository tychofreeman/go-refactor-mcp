package m

import "fmt"

func run() {
	fmt.Println("start")
	middle()
	fmt.Println("end")
}

// middle prints the middle lines.
func middle() {
	fmt.Println("one")
	fmt.Println("two")
}
