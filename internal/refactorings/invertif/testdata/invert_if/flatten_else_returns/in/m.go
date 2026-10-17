package m

import "fmt"

func run(ok bool) {
	if ok {
		fmt.Println("working")
		fmt.Println("done")
	} else {
		fmt.Println("skipped")
		return
	}
}
