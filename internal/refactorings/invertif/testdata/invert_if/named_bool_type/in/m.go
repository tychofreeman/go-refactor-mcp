package m

import "fmt"

type flag bool

func run(f flag) {
	if f {
		fmt.Println("then")
	} else {
		fmt.Println("else")
	}
}
