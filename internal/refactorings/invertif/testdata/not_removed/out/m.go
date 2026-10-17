package m

import "fmt"

func run(ok bool) {
	if ok {
		fmt.Println("else")
	} else {
		fmt.Println("then")
	}
}
