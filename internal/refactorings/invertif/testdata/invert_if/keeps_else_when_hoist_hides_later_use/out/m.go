package m

import "fmt"

var v = 1

func run(ok bool) {
	if v := 2; !ok {
		return
	} else {
		fmt.Println(v)
	}
	fmt.Println(v)
}
