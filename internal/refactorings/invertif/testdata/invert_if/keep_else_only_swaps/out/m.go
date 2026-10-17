package m

import "fmt"

func run(ok bool) {
	if !ok {
		return
	} else {
		fmt.Println("working")
	}
}
