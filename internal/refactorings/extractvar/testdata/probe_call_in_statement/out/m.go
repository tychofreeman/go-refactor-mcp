package main

import "fmt"

var n int

func next() int {
	n++
	return n
}

func main() {
	d := next() * 2
	r := 10 - d
	fmt.Println(r, n)
}
