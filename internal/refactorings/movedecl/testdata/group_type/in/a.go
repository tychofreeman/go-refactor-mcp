package m

type (
	id    int
	point struct {
		x, y int
	}
)

func Use(p point) id { return id(p.x) }
