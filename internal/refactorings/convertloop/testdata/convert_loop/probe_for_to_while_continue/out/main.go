package main

import "fmt"

func main() {
	ticks := 0
	tick := func(i int) int { ticks++; return i + 1 }
	i, j := 0, 0
	for i < 12 {
		if i%3 == 0 {
			i, j = tick(i), j+2
			continue
		}
		if i == 10 {
			break
		}
		fmt.Print(i, ":", j, " ")
		i, j = tick(i), j+2
	}
	fmt.Println(ticks)
}
