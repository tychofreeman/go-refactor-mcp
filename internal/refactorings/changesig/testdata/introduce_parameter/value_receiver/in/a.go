package m

import "fmt"

type point struct{ x, y int }

func (p point) label() string {
	return fmt.Sprint("x=", p.x)
}

func Use(pts []point, q *point) string {
	return pts[0].label() + q.label()
}
