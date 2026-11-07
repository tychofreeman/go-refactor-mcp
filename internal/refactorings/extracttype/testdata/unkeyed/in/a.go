package m

type point3 struct {
	x, y int
	z    int
}

func origin() point3 { return point3{1, 2, 3} }

func sum(p point3) int { return p.x + p.y + p.z }

func zero() point3 { return point3{} }
