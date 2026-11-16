package main

import "fmt"

var n int

func next() int {
	n++
	return n
}

func twice(x int) int { return x + x }

func main() {
	r := twice(next())
	fmt.Println(r, n)
}
