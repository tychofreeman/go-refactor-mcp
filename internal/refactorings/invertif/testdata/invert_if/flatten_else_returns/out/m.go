package m

import "fmt"

func run(ok bool) {
	if !ok {
		fmt.Println("skipped")
		return
	}
	fmt.Println("working")
	fmt.Println("done")
}
