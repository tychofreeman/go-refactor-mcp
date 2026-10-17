package m

import "fmt"

func run(a, b int) {
	var m int
	y := b
	if a > y {
		y = a
	}
	m = y * 1
	fmt.Println(m)
}
