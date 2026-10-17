package m

import "fmt"

var v = 1

func run(ok bool) {
	if v := 2; ok {
		fmt.Println(v)
	} else {
		return
	}
	fmt.Println(v)
}
