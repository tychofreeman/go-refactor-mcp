package m

import "fmt"

type point struct{ x, y int }

func label(p point, x int) string {
	return fmt.Sprint("x=", x)
}

func Use(pts []point) string {
	return label(pts[0], pts[0].x) + label(point{1, 2}, point{1, 2}.x)
}
