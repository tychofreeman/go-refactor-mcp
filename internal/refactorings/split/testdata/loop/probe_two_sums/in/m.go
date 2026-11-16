package main

import "fmt"

func main() {
	xs := []int{1, 2, 3, 4}
	evens, odds := 0, 0
	for _, x := range xs {
		if x%2 == 0 {
			evens += x
		}
		if x%2 == 1 {
			odds += x
		}
	}
	fmt.Println(evens, odds)
}
