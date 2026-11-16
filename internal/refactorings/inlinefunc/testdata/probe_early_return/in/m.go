package main

import "fmt"

func sign(x int) string {
	if x < 0 {
		return "negative"
	}
	if x == 0 {
		return "zero"
	}
	return "positive"
}

func main() {
	for _, x := range []int{-2, 0, 3} {
		s := sign(x)
		fmt.Println(x, s)
	}
}
