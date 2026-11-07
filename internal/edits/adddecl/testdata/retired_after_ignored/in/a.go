package m

// point is a point.
type point struct{ x, y int }

func origin() point { return point{} }

func unit() point { return point{1, 1} }
