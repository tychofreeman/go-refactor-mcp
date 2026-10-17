package m

import "fmt"

var limit = 10

func run(ok bool) {
	if !ok {
		return
	} else {
		limit := 3
		fmt.Println(limit)
	}
	fmt.Println(limit)
}
