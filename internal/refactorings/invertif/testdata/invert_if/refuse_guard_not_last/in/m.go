package m

import "fmt"

func run(ok bool) {
	if ok {
		fmt.Println("ok")
	}
	fmt.Println("after")
}
