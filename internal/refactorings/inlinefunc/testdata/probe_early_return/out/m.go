package main

import "fmt"

func main() {
	for _, x := range []int{-2, 0, 3} {
		var s string
		if x < 0 {
			s = "negative"
		} else {
			if x == 0 {
				s = "zero"
			} else {
				s = "positive"
			}
		}
		fmt.Println(x, s)
	}
}
