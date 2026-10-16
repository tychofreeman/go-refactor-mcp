package m

import "fmt"

type point struct{ x, y int }

func run() {
	var p point
	p.x = 1
	p.y = 2
	fmt.Println(p)
}
