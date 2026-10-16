package m

import "fmt"

type point struct{ x, y int }

func run() {
	var p point
	p = fill(p)
	fmt.Println(p)
}

func fill(p point) point {
	p.x = 1
	p.y = 2
	return p
}
