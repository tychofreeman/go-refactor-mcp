package main

import "fmt"

var calls []string

func a(v bool) bool {
	calls = append(calls, "a")
	return v
}

func b(v bool) bool {
	calls = append(calls, "b")
	return v
}

func main() {
	x, y := a(false), b(true)
	r := x && y
	fmt.Println(r, calls)
}
