package m

import "fmt"

func run(ok bool) int {
	n := 1
	if !ok {
		fmt.Println(n)
		panic("bad")
	}
	return n
}
