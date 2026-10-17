package m

import "fmt"

func run(ok bool) {
	n := 1
	fmt.Println(n)
	if ok {
		n := 2
		fmt.Println(n)
	}
}
