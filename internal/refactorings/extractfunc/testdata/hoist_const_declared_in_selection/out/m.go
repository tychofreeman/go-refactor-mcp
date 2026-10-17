package m

import "fmt"

const k = 3

func run() {
	step()
	fmt.Println(k + 1)
}

func step() {
	fmt.Println(k)
}
