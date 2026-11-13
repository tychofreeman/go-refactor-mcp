package main

import "fmt"

func main() {
	ticks := 0
	tick := func(i int) int { ticks++; return i + 1 }
	for i, j := 0, 0; i < 12; i, j = tick(i), j+2 {
		if i%3 == 0 {
			continue
		}
		if i == 10 {
			break
		}
		fmt.Print(i, ":", j, " ")
	}
	fmt.Println(ticks)
}
