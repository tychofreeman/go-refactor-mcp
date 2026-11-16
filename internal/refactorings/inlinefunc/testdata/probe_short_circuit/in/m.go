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

func both(x, y bool) bool { return x && y }

func main() {
	r := both(a(false), b(true))
	fmt.Println(r, calls)
}
