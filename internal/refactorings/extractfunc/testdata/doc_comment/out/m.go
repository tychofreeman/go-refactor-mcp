package m

import "fmt"

func run() {
	fmt.Println("start")
	middle()
	fmt.Println("end")
}

// middle prints the middle lines.
// They come between start and end.
func middle() {
	fmt.Println("one")
	fmt.Println("two")
}
