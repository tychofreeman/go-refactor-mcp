package m

import "fmt"

type point struct{ x, y int }

func run(q *point) {
	p := *q
	s := p.x
	total := s + p.y
	fmt.Println(total)
}
