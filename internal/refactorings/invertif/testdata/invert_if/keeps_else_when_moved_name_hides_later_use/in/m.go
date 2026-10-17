package m

import "fmt"

var limit = 10

func run(ok bool) {
	if ok {
		limit := 3
		fmt.Println(limit)
	} else {
		return
	}
	fmt.Println(limit)
}
