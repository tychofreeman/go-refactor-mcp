package m

import "fmt"

func run() {
	work()
}

func work() {
	defer fmt.Println("done")
	fmt.Println("work")
}
