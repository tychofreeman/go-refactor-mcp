package m

import "fmt"

type flag bool

func run(f flag) {
	if !f {
		fmt.Println("else")
	} else {
		fmt.Println("then")
	}
}
