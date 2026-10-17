package m

import "fmt"

type point struct{ x, y int }

func label(p point) string {
	return fmt.Sprint("x=", p.x)
}

func Use(pts []point) string {
	return label(pts[0]) + label(point{1, 2})
}
