package m

import "strconv"

// point is a point.
type point struct{ x, y int }

func origin() point { return point{} }

func unit() point { return point{1, 1} }

// String writes p as (x, y).
func (p point) String() string {
	return "(" + strconv.Itoa(p.x) + ", " + strconv.Itoa(p.y) + ")"
}
