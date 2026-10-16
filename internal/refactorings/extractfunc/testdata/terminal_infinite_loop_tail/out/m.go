package m

import "fmt"

func run() int {
	return step()
}

func step() int {
	for {
		fmt.Println("tick")
	}
}
