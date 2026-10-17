package m

import "fmt"

func run(ch chan bool) {
	if !<-ch {
		fmt.Println("else")
	} else {
		fmt.Println("then")
	}
}
