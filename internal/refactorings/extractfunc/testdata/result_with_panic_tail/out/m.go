package m

import "fmt"

func run(ok bool) int {
	n := 1
	if !ok {
		fail(n)
	}
	return n
}

func fail(n int) {
	fmt.Println(n)
	panic("bad")
}
