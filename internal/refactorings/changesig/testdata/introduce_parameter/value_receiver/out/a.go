package m

import "fmt"

type point struct{ x, y int }

func (p point) label(x int) string {
	return fmt.Sprint("x=", x)
}

func Use(pts []point, q *point) string {
	return pts[0].label(pts[0].x) + q.label(q.x)
}
