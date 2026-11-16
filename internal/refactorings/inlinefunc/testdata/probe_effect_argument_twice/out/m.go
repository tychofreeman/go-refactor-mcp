package main

import "fmt"

var n int

func next() int {
	n++
	return n
}

func main() {
	x := next()
	r := x + x
	fmt.Println(r, n)
}
