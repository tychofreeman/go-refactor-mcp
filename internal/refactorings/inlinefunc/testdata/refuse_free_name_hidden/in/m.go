package m

import "fmt"

var limit = 3

func run() {
	limit := 5
	show()
	fmt.Println(limit)
}

func show() {
	fmt.Println(limit)
}
