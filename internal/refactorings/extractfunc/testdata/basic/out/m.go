package m

import "fmt"

func run() {
	fmt.Println("start")
	middle()
	fmt.Println("end")
}

func middle() {
	fmt.Println("one")
	fmt.Println("two")
}
