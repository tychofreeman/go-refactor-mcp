package m

import "fmt"

type point struct{ x, y int }

func (p point) sum() int {
	s := p.x
	return s + p.y
}

func run(q *point) {
	total := q.sum()
	fmt.Println(total)
}
