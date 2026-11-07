package m

type xy struct {
	x, y int
}

type point3 struct {
	xy
	z int
}

func origin() point3 { return point3{xy{1, 2}, 3} }

func sum(p point3) int { return p.x + p.y + p.z }

func zero() point3 { return point3{} }
