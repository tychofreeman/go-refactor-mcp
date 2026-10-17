package m

import "fmt"

func run(ok bool) {
	if !ok {
		fmt.Println("then")
	} else {
		fmt.Println("else")
	}
}
