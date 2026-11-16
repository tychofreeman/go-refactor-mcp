package main

import "fmt"

var n int

func next() int {
	n++
	return n
}

func main() {
	r := 10 - next()*2
	fmt.Println(r, n)
}
