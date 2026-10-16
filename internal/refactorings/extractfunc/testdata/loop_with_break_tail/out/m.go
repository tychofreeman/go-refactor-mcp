package m

import "fmt"

func run() int {
	once()
	return 1
}

func once() {
	for {
		fmt.Println("once")
		break
	}
}
