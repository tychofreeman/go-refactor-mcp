package m

import "fmt"

func run() {
	if true {
		show()
	}
}

func show() {
	fmt.Println(`line one
    line two
line three`)
}
