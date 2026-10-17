package m

import "fmt"

var limit = 3

func run() {
	show()
	fmt.Println(limit)
}

func show() {
	limit := 2
	fmt.Println(limit)
}
