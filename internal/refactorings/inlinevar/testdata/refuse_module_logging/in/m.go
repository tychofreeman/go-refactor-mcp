package m

import "log"

func describe(n int) int {
	log.Printf("n=%d", n)
	return n
}

func f(a int) int {
	x := describe(a)
	return x + x
}
