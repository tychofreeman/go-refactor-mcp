package m

import "fmt"

func run(ok bool) {
	if !ok {
		return
	}
	fmt.Println("then")
}
