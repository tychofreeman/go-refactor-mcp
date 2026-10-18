package m

// point is a point.
type point struct{ x, y int }

// segment joins two points.
type segment struct{ from, to point }

func origin() point { return point{} }

func unit() point { return point{1, 1} }
