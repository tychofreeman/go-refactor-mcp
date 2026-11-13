package main

import "fmt"

func main() {
	i := 0
	for i < 9 {
		i++
		if i%3 == 0 {
			continue
		}
		fmt.Print(i, " ")
		i++
	}
	fmt.Println()
}
