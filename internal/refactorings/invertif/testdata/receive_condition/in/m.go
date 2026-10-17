package m

import "fmt"

func run(ch chan bool) {
	if <-ch {
		fmt.Println("then")
	} else {
		fmt.Println("else")
	}
}
