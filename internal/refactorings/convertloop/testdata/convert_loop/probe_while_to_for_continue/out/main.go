package main

import "fmt"

func main() {
	for i := 0; i < 9; {
		i++
		if i%3 == 0 {
			continue
		}
		fmt.Print(i, " ")
		i++
	}
	fmt.Println()
}
